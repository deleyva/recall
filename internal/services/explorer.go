package services

import (
	"database/sql"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/deleyva/recall/internal/models"
)

type ExplorerService struct {
	db        *sql.DB
	wikipedia *WikipediaService
}

func NewExplorerService(db *sql.DB, wikipedia *WikipediaService) *ExplorerService {
	return &ExplorerService{db: db, wikipedia: wikipedia}
}

func (s *ExplorerService) CreateGoal(userID, title, seedURL string, timeHorizon, dailyPace int) (*models.LearningGoal, error) {
	apiBase, _, lang, err := ParseWikiURL(seedURL)
	if err != nil {
		return nil, fmt.Errorf("invalid Wikipedia URL: %w", err)
	}

	// Fetch seed article summary
	summary, err := s.wikipedia.GetArticleSummaryAndLinks(seedURL)
	if err != nil {
		return nil, fmt.Errorf("fetch seed article: %w", err)
	}

	if title == "" {
		title = summary.Title
	}

	goalID := generateID()
	now := time.Now().UTC()

	_, err = s.db.Exec(`
		INSERT INTO learning_goals (id, user_id, title, seed_url, seed_title, seed_lang, time_horizon, daily_pace, status, thumb_url, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?)
	`, goalID, userID, title, seedURL, summary.Title, lang, timeHorizon, dailyPace, summary.ThumbURL, now.Format(time.RFC3339))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, fmt.Errorf("goal already exists for this article")
		}
		return nil, fmt.Errorf("create goal: %w", err)
	}

	// Create seed node (depth 0, queued, day 1)
	seedNodeID := generateID()
	day1 := 1
	_, err = s.db.Exec(`
		INSERT INTO graph_nodes (id, goal_id, wiki_title, wiki_url, summary, thumb_url, depth, status, scheduled_day, sort_order, relevance_score, created_at)
		VALUES (?, ?, ?, ?, ?, ?, 0, 'queued', ?, 0, 1.0, ?)
	`, seedNodeID, goalID, summary.Title, seedURL, summary.Extract, summary.ThumbURL, day1, now.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("create seed node: %w", err)
	}

	// Filter and score links
	var scoredLinks []struct {
		Title string
		Score float64
	}
	for _, link := range summary.Links {
		if IsFilteredLink(link) {
			continue
		}
		score := ScoreRelevance(summary.Title, link)
		scoredLinks = append(scoredLinks, struct {
			Title string
			Score float64
		}{link, score})
	}

	// Sort by relevance, take top 30
	sort.Slice(scoredLinks, func(i, j int) bool {
		return scoredLinks[i].Score > scoredLinks[j].Score
	})
	if len(scoredLinks) > 30 {
		scoredLinks = scoredLinks[:30]
	}

	// Batch fetch summaries for depth-1 nodes
	var titles []string
	for _, sl := range scoredLinks {
		titles = append(titles, sl.Title)
	}

	summaryMap := make(map[string]WikiSummary)
	for i := 0; i < len(titles); i += 10 {
		end := i + 10
		if end > len(titles) {
			end = len(titles)
		}
		batch, err := s.wikipedia.BatchGetSummaries(apiBase, titles[i:end])
		if err != nil {
			continue
		}
		for k, v := range batch {
			summaryMap[k] = v
		}
		if end < len(titles) {
			time.Sleep(200 * time.Millisecond)
		}
	}

	// Create depth-1 nodes and edges
	for order, sl := range scoredLinks {
		nodeID := generateID()
		wikiURL := fmt.Sprintf("https://%s.wikipedia.org/wiki/%s", lang, url.PathEscape(sl.Title))

		nodeSummary := ""
		nodeThumb := ""
		if ws, ok := summaryMap[sl.Title]; ok {
			nodeSummary = ws.Extract
			nodeThumb = ws.ThumbURL
		}

		_, err := s.db.Exec(`
			INSERT OR IGNORE INTO graph_nodes (id, goal_id, wiki_title, wiki_url, summary, thumb_url, depth, status, sort_order, relevance_score, created_at)
			VALUES (?, ?, ?, ?, ?, ?, 1, 'discovered', ?, ?, ?)
		`, nodeID, goalID, sl.Title, wikiURL, nodeSummary, nodeThumb, order, sl.Score, now.Format(time.RFC3339))
		if err != nil {
			continue
		}

		// Create edge from seed to this node
		edgeID := generateID()
		s.db.Exec(`
			INSERT OR IGNORE INTO graph_edges (id, goal_id, source_node_id, target_node_id)
			VALUES (?, ?, ?, ?)
		`, edgeID, goalID, seedNodeID, nodeID)
	}

	goal := &models.LearningGoal{
		ID:          goalID,
		UserID:      userID,
		Title:       title,
		SeedURL:     seedURL,
		SeedTitle:   summary.Title,
		SeedLang:    lang,
		TimeHorizon: timeHorizon,
		DailyPace:   dailyPace,
		Status:      "active",
		ThumbURL:    summary.ThumbURL,
		CreatedAt:   now,
	}
	return goal, nil
}

func (s *ExplorerService) GetGoal(goalID, userID string) (*models.LearningGoal, error) {
	goal := &models.LearningGoal{}
	var createdAt string
	err := s.db.QueryRow(`
		SELECT id, user_id, title, seed_url, seed_title, seed_lang, time_horizon, daily_pace, status, thumb_url, created_at
		FROM learning_goals WHERE id = ? AND user_id = ?
	`, goalID, userID).Scan(&goal.ID, &goal.UserID, &goal.Title, &goal.SeedURL, &goal.SeedTitle,
		&goal.SeedLang, &goal.TimeHorizon, &goal.DailyPace, &goal.Status, &goal.ThumbURL, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("goal not found: %w", err)
	}
	goal.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)

	// Compute counts
	s.db.QueryRow(`SELECT COUNT(*) FROM graph_nodes WHERE goal_id = ?`, goalID).Scan(&goal.TotalNodes)
	s.db.QueryRow(`SELECT COUNT(*) FROM graph_nodes WHERE goal_id = ? AND status = 'queued'`, goalID).Scan(&goal.QueuedNodes)
	s.db.QueryRow(`SELECT COUNT(*) FROM graph_nodes WHERE goal_id = ? AND status = 'added'`, goalID).Scan(&goal.AddedNodes)

	// Compute day number
	elapsed := time.Since(goal.CreatedAt).Hours() / 24
	goal.DayNumber = int(math.Min(math.Max(math.Ceil(elapsed), 1), float64(goal.TimeHorizon)))

	return goal, nil
}

func (s *ExplorerService) ListGoals(userID string) ([]models.LearningGoal, error) {
	rows, err := s.db.Query(`
		SELECT id, user_id, title, seed_url, seed_title, seed_lang, time_horizon, daily_pace, status, thumb_url, created_at
		FROM learning_goals WHERE user_id = ? AND status = 'active'
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var goals []models.LearningGoal
	for rows.Next() {
		var g models.LearningGoal
		var createdAt string
		if err := rows.Scan(&g.ID, &g.UserID, &g.Title, &g.SeedURL, &g.SeedTitle, &g.SeedLang,
			&g.TimeHorizon, &g.DailyPace, &g.Status, &g.ThumbURL, &createdAt); err != nil {
			continue
		}
		g.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)

		s.db.QueryRow(`SELECT COUNT(*) FROM graph_nodes WHERE goal_id = ?`, g.ID).Scan(&g.TotalNodes)
		s.db.QueryRow(`SELECT COUNT(*) FROM graph_nodes WHERE goal_id = ? AND status = 'queued'`, g.ID).Scan(&g.QueuedNodes)
		s.db.QueryRow(`SELECT COUNT(*) FROM graph_nodes WHERE goal_id = ? AND status = 'added'`, g.ID).Scan(&g.AddedNodes)

		elapsed := time.Since(g.CreatedAt).Hours() / 24
		g.DayNumber = int(math.Min(math.Max(math.Ceil(elapsed), 1), float64(g.TimeHorizon)))

		goals = append(goals, g)
	}
	return goals, nil
}

func (s *ExplorerService) DeleteGoal(goalID, userID string) error {
	result, err := s.db.Exec(`DELETE FROM learning_goals WHERE id = ? AND user_id = ?`, goalID, userID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("goal not found")
	}
	return nil
}

// GraphData holds nodes and edges for JSON response
type GraphData struct {
	Nodes []models.GraphNode `json:"nodes"`
	Edges []models.GraphEdge `json:"edges"`
}

func (s *ExplorerService) GetGraphData(goalID string) (*GraphData, error) {
	// Fetch nodes
	rows, err := s.db.Query(`
		SELECT id, goal_id, wiki_title, wiki_url, summary, thumb_url, depth, status, article_id, scheduled_day, sort_order, relevance_score, created_at
		FROM graph_nodes WHERE goal_id = ? ORDER BY depth, sort_order
	`, goalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []models.GraphNode
	for rows.Next() {
		var n models.GraphNode
		var createdAt string
		if err := rows.Scan(&n.ID, &n.GoalID, &n.WikiTitle, &n.WikiURL, &n.Summary, &n.ThumbURL,
			&n.Depth, &n.Status, &n.ArticleID, &n.ScheduledDay, &n.SortOrder, &n.RelevanceScore, &createdAt); err != nil {
			continue
		}
		n.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		nodes = append(nodes, n)
	}

	// Fetch edges
	edgeRows, err := s.db.Query(`
		SELECT id, goal_id, source_node_id, target_node_id
		FROM graph_edges WHERE goal_id = ?
	`, goalID)
	if err != nil {
		return nil, err
	}
	defer edgeRows.Close()

	var edges []models.GraphEdge
	for edgeRows.Next() {
		var e models.GraphEdge
		if err := edgeRows.Scan(&e.ID, &e.GoalID, &e.SourceNodeID, &e.TargetNodeID); err != nil {
			continue
		}
		edges = append(edges, e)
	}

	return &GraphData{Nodes: nodes, Edges: edges}, nil
}

func (s *ExplorerService) GetNode(goalID, nodeID string) (*models.GraphNode, error) {
	var n models.GraphNode
	var createdAt string
	err := s.db.QueryRow(`
		SELECT id, goal_id, wiki_title, wiki_url, summary, thumb_url, depth, status, article_id, scheduled_day, sort_order, relevance_score, created_at
		FROM graph_nodes WHERE id = ? AND goal_id = ?
	`, nodeID, goalID).Scan(&n.ID, &n.GoalID, &n.WikiTitle, &n.WikiURL, &n.Summary, &n.ThumbURL,
		&n.Depth, &n.Status, &n.ArticleID, &n.ScheduledDay, &n.SortOrder, &n.RelevanceScore, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("node not found: %w", err)
	}
	n.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &n, nil
}

func (s *ExplorerService) ExpandNode(goalID, nodeID string) ([]models.GraphNode, error) {
	// Get the parent node
	node, err := s.GetNode(goalID, nodeID)
	if err != nil {
		return nil, err
	}

	if node.Depth >= 3 {
		return nil, fmt.Errorf("maximum depth reached")
	}

	// Check total node count
	var count int
	s.db.QueryRow(`SELECT COUNT(*) FROM graph_nodes WHERE goal_id = ?`, goalID).Scan(&count)
	if count >= 150 {
		return nil, fmt.Errorf("maximum nodes reached (150)")
	}

	// Get goal for language
	var lang string
	s.db.QueryRow(`SELECT seed_lang FROM learning_goals WHERE id = ?`, goalID).Scan(&lang)
	if lang == "" {
		lang = "en"
	}
	apiBase := fmt.Sprintf("https://%s.wikipedia.org/w/api.php", lang)

	// Fetch links from this node's article
	summary, err := s.wikipedia.GetArticleSummaryAndLinks(node.WikiURL)
	if err != nil {
		return nil, fmt.Errorf("fetch links: %w", err)
	}

	// Get seed title for relevance scoring
	var seedTitle string
	s.db.QueryRow(`SELECT seed_title FROM learning_goals WHERE id = ?`, goalID).Scan(&seedTitle)

	// Filter and score
	var scoredLinks []struct {
		Title string
		Score float64
	}
	for _, link := range summary.Links {
		if IsFilteredLink(link) {
			continue
		}
		score := ScoreRelevance(seedTitle, link)
		scoredLinks = append(scoredLinks, struct {
			Title string
			Score float64
		}{link, score})
	}

	sort.Slice(scoredLinks, func(i, j int) bool {
		return scoredLinks[i].Score > scoredLinks[j].Score
	})

	// Limit by remaining capacity
	remaining := 150 - count
	if len(scoredLinks) > 30 {
		scoredLinks = scoredLinks[:30]
	}
	if len(scoredLinks) > remaining {
		scoredLinks = scoredLinks[:remaining]
	}

	// Batch fetch summaries
	var titles []string
	for _, sl := range scoredLinks {
		titles = append(titles, sl.Title)
	}

	summaryMap := make(map[string]WikiSummary)
	for i := 0; i < len(titles); i += 10 {
		end := i + 10
		if end > len(titles) {
			end = len(titles)
		}
		time.Sleep(200 * time.Millisecond)
		batch, err := s.wikipedia.BatchGetSummaries(apiBase, titles[i:end])
		if err != nil {
			continue
		}
		for k, v := range batch {
			summaryMap[k] = v
		}
	}

	now := time.Now().UTC()
	newDepth := node.Depth + 1
	var newNodes []models.GraphNode

	for order, sl := range scoredLinks {
		childID := generateID()
		wikiURL := fmt.Sprintf("https://%s.wikipedia.org/wiki/%s", lang, url.PathEscape(sl.Title))

		nodeSummary := ""
		nodeThumb := ""
		if ws, ok := summaryMap[sl.Title]; ok {
			nodeSummary = ws.Extract
			nodeThumb = ws.ThumbURL
		}

		result, err := s.db.Exec(`
			INSERT OR IGNORE INTO graph_nodes (id, goal_id, wiki_title, wiki_url, summary, thumb_url, depth, status, sort_order, relevance_score, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'discovered', ?, ?, ?)
		`, childID, goalID, sl.Title, wikiURL, nodeSummary, nodeThumb, newDepth, order, sl.Score, now.Format(time.RFC3339))
		if err != nil {
			continue
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			// Node already exists, get its ID for edge creation
			var existingID string
			s.db.QueryRow(`SELECT id FROM graph_nodes WHERE goal_id = ? AND wiki_url = ?`, goalID, wikiURL).Scan(&existingID)
			if existingID != "" {
				edgeID := generateID()
				s.db.Exec(`INSERT OR IGNORE INTO graph_edges (id, goal_id, source_node_id, target_node_id) VALUES (?, ?, ?, ?)`,
					edgeID, goalID, nodeID, existingID)
			}
			continue
		}

		// Create edge
		edgeID := generateID()
		s.db.Exec(`INSERT OR IGNORE INTO graph_edges (id, goal_id, source_node_id, target_node_id) VALUES (?, ?, ?, ?)`,
			edgeID, goalID, nodeID, childID)

		newNodes = append(newNodes, models.GraphNode{
			ID:             childID,
			GoalID:         goalID,
			WikiTitle:      sl.Title,
			WikiURL:        wikiURL,
			Summary:        nodeSummary,
			ThumbURL:       nodeThumb,
			Depth:          newDepth,
			Status:         "discovered",
			SortOrder:      order,
			RelevanceScore: sl.Score,
			CreatedAt:      now,
		})
	}

	return newNodes, nil
}

func (s *ExplorerService) QueueNode(goalID, nodeID string, day int) error {
	result, err := s.db.Exec(`
		UPDATE graph_nodes SET status = 'queued', scheduled_day = ?
		WHERE id = ? AND goal_id = ? AND status = 'discovered'
	`, day, nodeID, goalID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("node not found or already queued/added")
	}
	return nil
}

func (s *ExplorerService) UnqueueNode(goalID, nodeID string) error {
	result, err := s.db.Exec(`
		UPDATE graph_nodes SET status = 'discovered', scheduled_day = NULL
		WHERE id = ? AND goal_id = ? AND status = 'queued'
	`, nodeID, goalID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("node not found or not queued")
	}
	return nil
}

func (s *ExplorerService) AddNodeToReadList(goalID, nodeID string, articles *ArticleService, userID string) error {
	node, err := s.GetNode(goalID, nodeID)
	if err != nil {
		return err
	}

	// Create article in the To Read list
	article, err := articles.Create(userID, node.WikiURL)
	if err != nil {
		// If already exists, try to find it
		if strings.Contains(err.Error(), "already") {
			var articleID string
			s.db.QueryRow(`SELECT id FROM articles WHERE user_id = ? AND url = ?`, userID, node.WikiURL).Scan(&articleID)
			if articleID != "" {
				_, err = s.db.Exec(`UPDATE graph_nodes SET status = 'added', article_id = ? WHERE id = ? AND goal_id = ?`,
					articleID, nodeID, goalID)
				return err
			}
		}
		return fmt.Errorf("add to read list: %w", err)
	}

	_, err = s.db.Exec(`UPDATE graph_nodes SET status = 'added', article_id = ? WHERE id = ? AND goal_id = ?`,
		article.ID, nodeID, goalID)
	return err
}

func (s *ExplorerService) AutoSchedule(goalID string) error {
	// Get goal
	var timeHorizon, dailyPace, dayNumber int
	var createdAt string
	err := s.db.QueryRow(`SELECT time_horizon, daily_pace, created_at FROM learning_goals WHERE id = ?`, goalID).
		Scan(&timeHorizon, &dailyPace, &createdAt)
	if err != nil {
		return err
	}

	created, _ := time.Parse(time.RFC3339, createdAt)
	elapsed := time.Since(created).Hours() / 24
	dayNumber = int(math.Min(math.Max(math.Ceil(elapsed), 1), float64(timeHorizon)))

	// Get queued nodes sorted by depth then relevance
	rows, err := s.db.Query(`
		SELECT id FROM graph_nodes
		WHERE goal_id = ? AND status = 'queued'
		ORDER BY depth ASC, relevance_score DESC
	`, goalID)
	if err != nil {
		return err
	}
	defer rows.Close()

	var nodeIDs []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		nodeIDs = append(nodeIDs, id)
	}

	// Distribute across remaining days
	remainingDays := timeHorizon - dayNumber + 1
	if remainingDays <= 0 {
		remainingDays = 1
	}

	for i, nodeID := range nodeIDs {
		day := dayNumber + (i / dailyPace)
		if day > timeHorizon {
			day = timeHorizon
		}
		s.db.Exec(`UPDATE graph_nodes SET scheduled_day = ? WHERE id = ? AND goal_id = ?`, day, nodeID, goalID)
	}

	return nil
}

// GetTimelineData returns nodes grouped by scheduled day
func (s *ExplorerService) GetTimelineData(goalID string, timeHorizon int) map[int][]models.GraphNode {
	timeline := make(map[int][]models.GraphNode)

	rows, err := s.db.Query(`
		SELECT id, goal_id, wiki_title, wiki_url, summary, thumb_url, depth, status, article_id, scheduled_day, sort_order, relevance_score, created_at
		FROM graph_nodes
		WHERE goal_id = ? AND scheduled_day IS NOT NULL
		ORDER BY scheduled_day, sort_order
	`, goalID)
	if err != nil {
		return timeline
	}
	defer rows.Close()

	for rows.Next() {
		var n models.GraphNode
		var createdAt string
		if err := rows.Scan(&n.ID, &n.GoalID, &n.WikiTitle, &n.WikiURL, &n.Summary, &n.ThumbURL,
			&n.Depth, &n.Status, &n.ArticleID, &n.ScheduledDay, &n.SortOrder, &n.RelevanceScore, &createdAt); err != nil {
			continue
		}
		n.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		if n.ScheduledDay != nil {
			timeline[*n.ScheduledDay] = append(timeline[*n.ScheduledDay], n)
		}
	}

	return timeline
}
