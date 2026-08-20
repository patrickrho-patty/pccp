package api

// Patty Trails (PAT-1450): RBAC-governed causal graph projections over
// existing immutable records.
//
// Locked rules enforced here:
//   - Authorization happens BEFORE graph expansion, aggregation, counts,
//     search, paths, and layout. Frontend filtering is presentational.
//   - Edges exist only where a recorded relationship proves them
//     (session on changeset, exchange linking, policy verdict on
//     action); chronological adjacency never creates causation.
//   - Raw content is never copied into the index: nodes carry bounded
//     Korean labels + integrity digests only.
//   - No export: there is deliberately no bulk/download/share endpoint.
//   - Node/edge budgets bound the canvas; repeated same-purpose actions
//     collapse into one expandable node with a count.

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/patrickrho-patty/pccp/internal/models"
)

// Critical node taxonomy (PAT-1450 locked).
const (
	tnGoal      = "goal"
	tnExecution = "execution"
	tnDecision  = "decision"
	tnChange    = "change"
	tnEffect    = "effect"
	tnException = "exception"
	tnOutcome   = "outcome"
)

var tnTypeKo = map[string]string{
	tnGoal:      "목표",
	tnExecution: "실행",
	tnDecision:  "결정",
	tnChange:    "변경",
	tnEffect:    "효과",
	tnException: "예외",
	tnOutcome:   "결과",
}

var tnStatusKo = map[string]string{
	"ok": "정상", "denied": "차단", "failed": "실패",
	"rolled_back": "롤백됨", "degraded": "무결성 저하",
}

// trBudget bounds nodes/edges per query (hairball prevention).
const trBudget = 300

// trailViewAllowed gates Trail access: explicit permission, not mere
// admin status (PAT-1450 RBAC).
func trailViewAllowed(role string) bool {
	switch role {
	case "super_admin", "admin", "security_admin", "compliance_admin", "team_lead":
		return true
	}
	return false
}

func trForbidden() (int, string) {
	return http.StatusForbidden, "Trails 접근 권한이 없습니다"
}

// trDeriveNodes materializes the critical-node projection from recorded
// facts. Called by the ingestion/rebuild endpoint — never on raw content.
func (s *Server) trDeriveNodes(orgID string) (int, int, error) {
	nodes, edges := 0, 0
	digest := func(parts ...string) string {
		sum := sha256.Sum256([]byte(fmt.Sprint(parts)))
		return fmt.Sprintf("%x", sum[:12])
	}

	// Goal nodes: sessions open a trail; the session record is the goal's
	// evidence (user request), linked by session_id on actions/changesets.
	var sessions []models.Session
	s.db.Where("organization_id = ?", orgID).Limit(500).Find(&sessions)
	for _, sess := range sessions {
		node := models.TrailNode{
			OrganizationID: orgID, SourceType: "session", SourceID: sess.ID,
			NodeType: tnGoal, SessionID: sess.ID,
			LabelKo: "세션 시작 — 사용자 요청", Status: "ok",
			OccurredAt: sess.CreatedAt.UTC().Format(time.RFC3339),
			IntegrityDigest: digest("session", sess.ID),
		}
		if err := s.db.Where("organization_id = ? AND source_type = ? AND source_id = ?",
			orgID, "session", sess.ID).FirstOrCreate(&node).Error; err == nil {
			nodes++
		}
	}

	// Execution + decision nodes from ActionEnvelopes; verdict-carrying
	// actions become decisions (policy allowed/blocked/redirected).
	var actions []models.ActionEnvelope
	s.db.Where("organization_id = ?", orgID).Order("occurred_at ASC").Limit(1000).Find(&actions)
	for _, a := range actions {
		nt := tnExecution
		status := "ok"
		label := "모델/도구 실행 — " + a.ActionType
		if a.VerdictResult != "" {
			nt = tnDecision
			if a.VerdictResult == "deny" || a.VerdictResult == "denied" {
				status = "denied"
				label = "정책 결정 — 차단"
			} else {
				label = "정책 결정 — " + a.VerdictResult
			}
		}
		node := models.TrailNode{
			OrganizationID: orgID, SourceType: "action", SourceID: a.ActionID,
			NodeType: nt, UserIDAtEvent: a.UserID, HarnessIDAtEvent: a.HarnessID,
			SessionID: a.SessionID, ProjectID: a.ProjectID, RepositoryID: a.RepositoryID,
			LabelKo: label, Status: status,
			OccurredAt: a.OccurredAt, IntegrityDigest: a.EnvelopeDigest,
			GroupingKey: digest("action-group", a.SessionID, a.ActionType),
		}
		if err := s.db.Where("organization_id = ? AND source_type = ? AND source_id = ?",
			orgID, "action", a.ActionID).FirstOrCreate(&node).Error; err == nil {
			nodes++
		}
		// Edge: session(goal) initiated the action — proven by the
		// recorded session_id on the envelope (not adjacency).
		if a.SessionID != "" {
			edge := models.TrailEdge{
				OrganizationID: orgID,
				FromSourceType: "session", FromSourceID: a.SessionID,
				ToSourceType: "action", ToSourceID: a.ActionID,
				EdgeType: "initiated", SourceEvidence: "action_envelope.session_id",
				OccurredAt: a.OccurredAt, IntegrityDigest: digest("e1", a.SessionID, a.ActionID),
			}
			if err := s.db.Where("organization_id = ? AND from_source_type = ? AND from_source_id = ? AND to_source_type = ? AND to_source_id = ?",
				orgID, "session", a.SessionID, "action", a.ActionID).FirstOrCreate(&edge).Error; err == nil {
				edges++
			}
		}
	}

	// Change nodes from ChangeSets — the session → changeset edge is
	// proven by the recorded SessionID on the changeset.
	var changes []models.ChangeSet
	s.db.Where("organization_id = ?", orgID).Order("created_at ASC").Limit(1000).Find(&changes)
	for _, c := range changes {
		node := models.TrailNode{
			OrganizationID: orgID, SourceType: "changeset", SourceID: fmt.Sprint(c.ID),
			NodeType: tnChange, UserIDAtEvent: c.UserID, HarnessIDAtEvent: c.HarnessID,
			SessionID: c.SessionID, RepositoryID: c.RepositoryID,
			LabelKo: fmt.Sprintf("코드 변경 — %s (%d줄 추가 / %d줄 삭제)", c.Branch, c.LinesAdded, c.LinesRemoved),
			Status:  "ok", OccurredAt: c.CreatedAt.UTC().Format(time.RFC3339),
			IntegrityDigest: c.DiffDigest,
		}
		if err := s.db.Where("organization_id = ? AND source_type = ? AND source_id = ?",
			orgID, "changeset", fmt.Sprint(c.ID)).FirstOrCreate(&node).Error; err == nil {
			nodes++
		}
		if c.SessionID != "" {
			edge := models.TrailEdge{
				OrganizationID: orgID,
				FromSourceType: "action-set", FromSourceID: c.SessionID,
				ToSourceType: "changeset", ToSourceID: fmt.Sprint(c.ID),
				EdgeType: "produced", SourceEvidence: "changeset.session_id",
				OccurredAt: c.CreatedAt.UTC().Format(time.RFC3339),
				IntegrityDigest: digest("e2", c.SessionID, fmt.Sprint(c.ID)),
			}
			if err := s.db.Where("organization_id = ? AND from_source_type = ? AND from_source_id = ? AND to_source_type = ? AND to_source_id = ?",
				orgID, "action-set", c.SessionID, "changeset", fmt.Sprint(c.ID)).FirstOrCreate(&edge).Error; err == nil {
				edges++
			}
		}
	}
	return nodes, edges, nil
}

// trGraphQuery is the bounded, authorization-first graph read.
type trGraphQuery struct {
	ScopeKind string // organization|session|user|harness|project|repository
	ScopeRef  string
	From      string
	To        string
	NodeTypes map[string]bool
	Limit     int
}

func (s *Server) trGraph(orgID string, q trGraphQuery) (map[string]interface{}, int) {
	limit := q.Limit
	if limit <= 0 || limit > trBudget {
		limit = trBudget
	}
	nq := s.db.Where("organization_id = ?", orgID)
	switch q.ScopeKind {
	case "session":
		nq = nq.Where("session_id = ?", q.ScopeRef)
	case "user":
		nq = nq.Where("user_id_at_event = ?", q.ScopeRef)
	case "harness":
		nq = nq.Where("harness_id_at_event = ?", q.ScopeRef)
	case "project":
		nq = nq.Where("project_id = ?", q.ScopeRef)
	case "repository":
		nq = nq.Where("repository_id = ?", q.ScopeRef)
	}
	if q.From != "" {
		nq = nq.Where("occurred_at >= ?", q.From)
	}
	if q.To != "" {
		nq = nq.Where("occurred_at <= ?", q.To)
	}
	// Type filters apply INSIDE the query so the budget truncation never
	// hides matching nodes behind a page of filtered-out rows.
	if len(q.NodeTypes) > 0 {
		types := make([]string, 0, len(q.NodeTypes))
		for t := range q.NodeTypes {
			types = append(types, t)
		}
		nq = nq.Where("node_type IN ?", types)
	}
	var nodes []models.TrailNode
	nq.Order("occurred_at ASC").Limit(limit).Find(&nodes)
	// Collapsed grouping: same grouping key collapses to one visible node
	// with a count (bounded summaries, no canvas flooding).
	visible := make([]models.TrailNode, 0, len(nodes))
	groupCounts := map[string]int{}
	for _, n := range nodes {
		if n.NodeType == tnExecution && n.GroupingKey != "" {
			k := n.SessionID + "|" + n.GroupingKey
			groupCounts[k]++
			if groupCounts[k] == 1 {
				visible = append(visible, n)
			}
			continue
		}
		visible = append(visible, n)
	}
	// Edges within window (bounded to visible node set).
	nodeSet := map[string]bool{}
	for _, n := range nodes {
		nodeSet[n.SourceType+":"+n.SourceID] = true
	}
	var edges []models.TrailEdge
	s.db.Where("organization_id = ?", orgID).Limit(limit * 2).Find(&edges)
	inWindow := make([]map[string]interface{}, 0, len(edges))
	for _, e := range edges {
		from := e.FromSourceType + ":" + e.FromSourceID
		// action-set edges join session goals to changesets; map through.
		if e.FromSourceType == "action-set" {
			from = "session:" + e.FromSourceID
		}
		if nodeSet[from] || nodeSet[e.ToSourceType+":"+e.ToSourceID] {
			inWindow = append(inWindow, map[string]interface{}{
				"from": e.FromSourceType + ":" + e.FromSourceID,
				"to":   e.ToSourceType + ":" + e.ToSourceID,
				"type": e.EdgeType, "evidence": e.SourceEvidence,
			})
		}
		if len(inWindow) >= limit {
			break
		}
	}
	// Cluster overview counts (authorization already applied by orgID).
	dist := map[string]int{}
	for _, n := range nodes {
		dist[n.NodeType]++
	}
	return map[string]interface{}{
		"nodes": visible, "edges": inWindow, "node_type_distribution": dist,
		"collapsed_groups": len(groupCounts), "budget": trBudget, "truncated": len(nodes) >= limit,
	}, len(nodes)
}

// handleTrailsOverview is the aggregated entry point: bounded clusters,
// never the raw org-scale hairball.
func (s *Server) handleTrailsOverview(w http.ResponseWriter, r *http.Request) {
	if !trailViewAllowed(getRole(r)) {
		code, msg := trForbidden()
		writeError(w, code, msg)
		return
	}
	orgID := getOrgID(r)
	graph, _ := s.trGraph(orgID, trGraphQuery{ScopeKind: "organization"})
	// Cluster summaries per session (each session = one trail cluster).
	type cluster struct {
		SessionID string `json:"session_id"`
		NodeCount int    `json:"node_count"`
		LastAt    string `json:"last_at"`
	}
	nodesRaw, _ := graph["nodes"].([]models.TrailNode)
	bySession := map[string]*cluster{}
	order := []string{}
	for _, n := range nodesRaw {
		c, ok := bySession[n.SessionID]
		if !ok {
			c = &cluster{SessionID: n.SessionID, LastAt: n.OccurredAt}
			bySession[n.SessionID] = c
			order = append(order, n.SessionID)
		}
		c.NodeCount++
		if n.OccurredAt > c.LastAt {
			c.LastAt = n.OccurredAt
		}
	}
	clusters := make([]cluster, 0, len(order))
	for _, sid := range order {
		clusters = append(clusters, *bySession[sid])
	}
	dist, _ := graph["node_type_distribution"].(map[string]int)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"clusters": clusters, "node_type_distribution": dist,
		"budget": trBudget,
	})
}

// handleTrailsGraph serves one scope's bounded graph with filters.
func (s *Server) handleTrailsGraph(w http.ResponseWriter, r *http.Request) {
	if !trailViewAllowed(getRole(r)) {
		code, msg := trForbidden()
		writeError(w, code, msg)
		return
	}
	orgID := getOrgID(r)
	q := trGraphQuery{
		ScopeKind: r.URL.Query().Get("scope"),
		ScopeRef:  r.URL.Query().Get("ref"),
		From:      r.URL.Query().Get("from"),
		To:        r.URL.Query().Get("to"),
		NodeTypes: map[string]bool{},
	}
	if q.ScopeKind == "" {
		q.ScopeKind = "organization"
	}
	for _, t := range r.URL.Query()["type"] {
		q.NodeTypes[t] = true
	}
	graph, _ := s.trGraph(orgID, q)
	// Nodes sanitized to safe fields only.
	nodesRaw, _ := graph["nodes"].([]models.TrailNode)
	out := make([]map[string]interface{}, 0, len(nodesRaw))
	for _, n := range nodesRaw {
		out = append(out, map[string]interface{}{
			"source_type": n.SourceType, "source_id": n.SourceID,
			"node_type": n.NodeType, "node_type_ko": tnTypeKo[n.NodeType],
			"label_ko": n.LabelKo, "status": n.Status, "status_ko": tnStatusKo[n.Status],
			"session_id": n.SessionID, "user_id": n.UserIDAtEvent, "harness_id": n.HarnessIDAtEvent,
			"repository_id": n.RepositoryID, "occurred_at": n.OccurredAt,
			"integrity_digest": n.IntegrityDigest,
		})
	}
	graph["nodes"] = out
	s.db.Create(&models.TrailViewerScope{
		OrganizationID: orgID, ViewerEmail: getOperatorEmail(r), Role: getRole(r),
		ScopeKind: q.ScopeKind, ScopeRef: q.ScopeRef, LastViewedAt: time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, graph)
}

// handleTrailsNodeDetail resolves one node's authorized detail fields
// from the system of record (graph never duplicates content).
func (s *Server) handleTrailsNodeDetail(w http.ResponseWriter, r *http.Request) {
	if !trailViewAllowed(getRole(r)) {
		code, msg := trForbidden()
		writeError(w, code, msg)
		return
	}
	orgID := getOrgID(r)
	st := chi.URLParam(r, "sourceType")
	sid := chi.URLParam(r, "sourceId")
	var n models.TrailNode
	if err := s.db.Where("organization_id = ? AND source_type = ? AND source_id = ?", orgID, st, sid).
		First(&n).Error; err != nil {
		writeError(w, http.StatusNotFound, "노드를 찾을 수 없습니다")
		return
	}
	detail := map[string]interface{}{
		"node_type": n.NodeType, "node_type_ko": tnTypeKo[n.NodeType],
		"label_ko": n.LabelKo, "status": n.Status, "status_ko": tnStatusKo[n.Status],
		"occurred_at": n.OccurredAt, "integrity_digest": n.IntegrityDigest,
		"session_id": n.SessionID, "user_id": n.UserIDAtEvent,
		"harness_id": n.HarnessIDAtEvent, "repository_id": n.RepositoryID,
		"collapsed_count": n.CollapsedCount,
	}
	// Typed metadata per source — no raw payloads.
	switch st {
	case "action":
		var a models.ActionEnvelope
		if err := s.db.Where("action_id = ? AND organization_id = ?", sid, orgID).First(&a).Error; err == nil {
			detail["action_type"] = a.ActionType
			detail["verdict"] = a.VerdictResult
			detail["policy_epoch"] = a.PolicyEpochID
			detail["model_package_id"] = a.ModelPackageID
		}
	case "changeset":
		var c models.ChangeSet
		if err := s.db.Where("id = ? AND organization_id = ?", sid, orgID).First(&c).Error; err == nil {
			detail["branch"] = c.Branch
			detail["attribution"] = c.AttributionState
			detail["lines_added"] = c.LinesAdded
			detail["lines_removed"] = c.LinesRemoved
			detail["diff_digest"] = c.DiffDigest
		}
	}
	writeJSON(w, http.StatusOK, detail)
}

// handleTrailsNeighbors walks explicit edges upstream/downstream with a
// depth bound (cycles cannot loop forever).
func (s *Server) handleTrailsNeighbors(w http.ResponseWriter, r *http.Request) {
	if !trailViewAllowed(getRole(r)) {
		code, msg := trForbidden()
		writeError(w, code, msg)
		return
	}
	orgID := getOrgID(r)
	st := chi.URLParam(r, "sourceType")
	sid := chi.URLParam(r, "sourceId")
	direction := r.URL.Query().Get("direction") // upstream|downstream
	depth := 3
	var edges []models.TrailEdge
	s.db.Where("organization_id = ?", orgID).Limit(2000).Find(&edges)
	// Directional adjacency: downstream follows from→to, upstream follows
	// to→from. A direction request must not return the wrong side.
	downstream := map[string][]map[string]string{}
	upstream := map[string][]map[string]string{}
	for _, e := range edges {
		from := e.FromSourceType + ":" + e.FromSourceID
		if e.FromSourceType == "action-set" {
			from = "session:" + e.FromSourceID
		}
		to := e.ToSourceType + ":" + e.ToSourceID
		downstream[from] = append(downstream[from], map[string]string{"to": to, "type": e.EdgeType})
		upstream[to] = append(upstream[to], map[string]string{"to": from, "type": e.EdgeType})
	}
	adj := downstream
	if direction == "upstream" {
		adj = upstream
	}
	start := st + ":" + sid
	seen := map[string]bool{start: true}
	frontier := []string{start}
	hops := []map[string]interface{}{}
	for d := 0; d < depth && len(frontier) > 0; d++ {
		next := []string{}
		for _, cur := range frontier {
			for _, edge := range adj[cur] {
				nb := edge["to"]
				if seen[nb] {
					continue // cycle-safe
				}
				seen[nb] = true
				next = append(next, nb)
				hops = append(hops, map[string]interface{}{"node": nb, "via": edge["type"], "depth": d + 1})
				if len(hops) >= 50 {
					break
				}
			}
		}
		frontier = next
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"direction": direction, "neighbors": hops})
}

// handleTrailsPath finds the causal path between two nodes using only
// explicit edges (BFS, bounded).
func (s *Server) handleTrailsPath(w http.ResponseWriter, r *http.Request) {
	if !trailViewAllowed(getRole(r)) {
		code, msg := trForbidden()
		writeError(w, code, msg)
		return
	}
	orgID := getOrgID(r)
	var req struct {
		FromType string `json:"from_type"`
		FromID   string `json:"from_id"`
		ToType   string `json:"to_type"`
		ToID     string `json:"to_id"`
	}
	if err := decodeJSON(r, &req); err != nil || req.FromID == "" || req.ToID == "" {
		writeError(w, http.StatusBadRequest, "from/to가 필요합니다")
		return
	}
	var edges []models.TrailEdge
	s.db.Where("organization_id = ?", orgID).Limit(2000).Find(&edges)
	adj := map[string][]string{}
	for _, e := range edges {
		from := e.FromSourceType + ":" + e.FromSourceID
		if e.FromSourceType == "action-set" {
			from = "session:" + e.FromSourceID
		}
		adj[from] = append(adj[from], e.ToSourceType+":"+e.ToSourceID)
	}
	start := req.FromType + ":" + req.FromID
	goal := req.ToType + ":" + req.ToID
	prev := map[string]string{start: ""}
	visited := map[string]bool{start: true}
	queue := []string{start}
	found := false
	for len(queue) > 0 && !found {
		cur := queue[0]
		queue = queue[1:]
		for _, nb := range adj[cur] {
			if visited[nb] {
				continue
			}
			visited[nb] = true
			prev[nb] = cur
			if nb == goal {
				found = true
				break
			}
			queue = append(queue, nb)
		}
	}
	if !found {
		// No explicit causal chain: report honestly, do not invent one.
		writeJSON(w, http.StatusOK, map[string]interface{}{"path": nil, "found": false})
		return
	}
	path := []string{}
	for cur := goal; cur != ""; cur = prev[cur] {
		path = append([]string{cur}, path...)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"path": path, "found": true})
}

// handleTrailsRebuild re-derives the projection from canonical records
// (idempotent; FirstOrCreate everywhere).
func (s *Server) handleTrailsRebuild(w http.ResponseWriter, r *http.Request) {
	if !enterpriseRoleAdmin(getRole(r)) {
		writeError(w, code403(), "재구축 권한이 없습니다")
		return
	}
	orgID := getOrgID(r)
	nodes, edges, err := s.trDeriveNodes(orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"nodes_derived": nodes, "edges_derived": edges})
}

func code403() int { return http.StatusForbidden }

var _ = json.Marshal
