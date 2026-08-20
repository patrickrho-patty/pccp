package scheduler

// replay.go implements the offline half of PAT-1445 research evaluation:
// replay a captured trace through any versioned router and compare a
// candidate's decisions against the frozen baseline — the same agreement
// metrics shadow mode records live (§simulation and replay, §shadow mode).
// Replays consume governed traces only; no content ever enters the path.

// ReplaySummary aggregates one router's decisions over a trace.
type ReplaySummary struct {
	RouterVersion string         `json:"router_version"`
	Decisions     int            `json:"decisions"`
	Errors        int            `json:"errors"`
	ByWorker      map[string]int `json:"by_worker"`
	TotalOverlap  int            `json:"total_overlap_tokens"`
	MeanCost      float64        `json:"mean_cost"`
}

// routeRequestFor rebuilds the router input from an arrived event. The
// trace carries exactly the bounded features the production router is
// authorized to see, so replayed decisions are comparable to live ones.
func routeRequestFor(e TraceEvent) RouteRequest {
	return RouteRequest{
		Model:                e.Model,
		Namespace:            e.Tenant,
		Region:               e.Region,
		InputTokens:          e.InputTokens,
		CachedTokens:         e.CachedTokens,
		ExpectedOutputTokens: e.ExpectedOutputTokens,
		RequestClass:         e.Class,
	}
}

// ReplayDecisions runs every arrived event in the trace through r and
// aggregates the placements. Errors (no eligible worker) are counted, not
// fatal — a policy that strands requests must show up in the report.
func ReplayDecisions(events []TraceEvent, r Router) ReplaySummary {
	s := ReplaySummary{RouterVersion: r.Version(), ByWorker: make(map[string]int)}
	var costSum float64
	for _, e := range events {
		if e.Stage != TraceArrived {
			continue
		}
		dec, err := r.Route(routeRequestFor(e))
		if err != nil {
			s.Errors++
			continue
		}
		s.Decisions++
		s.ByWorker[dec.WorkerID]++
		s.TotalOverlap += dec.OverlapTokens
		costSum += dec.Cost
	}
	if s.Decisions > 0 {
		s.MeanCost = costSum / float64(s.Decisions)
	}
	return s
}

// ComparisonReport is the baseline-vs-candidate outcome: how often the
// candidate picks the baseline's worker (the replay analog of the live
// ShadowRecord stream).
type ComparisonReport struct {
	BaselineVersion  string  `json:"baseline_version"`
	CandidateVersion string  `json:"candidate_version"`
	Compared         int     `json:"compared"`
	Agree            int     `json:"agree"`
	CandidateErrors  int     `json:"candidate_errors"`
	AgreementRate    float64 `json:"agreement_rate"`
}

// CompareRouters replays the trace through both routers and reports
// decision agreement. Events the baseline cannot route are skipped (there
// is no baseline decision to compare against); candidate errors are
// counted so a less-available policy is visible.
func CompareRouters(events []TraceEvent, baseline, candidate Router) ComparisonReport {
	rep := ComparisonReport{
		BaselineVersion:  baseline.Version(),
		CandidateVersion: candidate.Version(),
	}
	for _, e := range events {
		if e.Stage != TraceArrived {
			continue
		}
		req := routeRequestFor(e)
		b, err := baseline.Route(req)
		if err != nil {
			continue
		}
		rep.Compared++
		c, err := candidate.Route(req)
		if err != nil {
			rep.CandidateErrors++
			continue
		}
		if c.WorkerID == b.WorkerID {
			rep.Agree++
		}
	}
	if rep.Compared > 0 {
		rep.AgreementRate = float64(rep.Agree) / float64(rep.Compared)
	}
	return rep
}
