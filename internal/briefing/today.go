// Package briefing produces the structured daily summary that backs
// `coggo today`. v0.1 uses pure aggregation — no LLM, no embeddings.
package briefing

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lunguini/coggo/internal/peer"
	"github.com/lunguini/coggo/internal/types"
)

// CoggoPeerName is the conventional name of the self-peer (see spec §1).
const CoggoPeerName = "coggo"

// Briefing is the structured aggregation rendered by `coggo today`.
type Briefing struct {
	Date  time.Time     `json:"date"`
	Peers []PeerSection `json:"peers"`
}

// PeerSection groups all sections for a single peer.
type PeerSection struct {
	Name     string    `json:"name"`
	Sections []Section `json:"sections"`
}

// Section is a titled list of summary lines.
type Section struct {
	Title string   `json:"title"`
	Lines []string `json:"lines"`
}

// Generate produces the briefing for one peer (peerName != "") or all peers.
// `now` is the reference moment (used for "last 7 days" / "stale > 14 days").
func Generate(ctx context.Context, registry *peer.Registry, store types.Store, peerName string, now time.Time) (*Briefing, error) {
	if registry == nil {
		return nil, fmt.Errorf("briefing: nil registry")
	}
	if store == nil {
		return nil, fmt.Errorf("briefing: nil store")
	}
	now = now.UTC()
	b := &Briefing{Date: now}

	var peers []*types.Peer
	if peerName == "" {
		peers = registry.List()
	} else {
		p, err := registry.Resolve(peerName)
		if err != nil {
			return nil, fmt.Errorf("briefing: %w", err)
		}
		peers = []*types.Peer{p}
	}

	for _, p := range peers {
		ps, err := buildPeerSection(ctx, store, p, now)
		if err != nil {
			return nil, err
		}
		b.Peers = append(b.Peers, ps)
	}
	return b, nil
}

func buildPeerSection(ctx context.Context, store types.Store, p *types.Peer, now time.Time) (PeerSection, error) {
	ps := PeerSection{Name: p.Name}

	// Open goals
	goals, err := store.QueryEntities(ctx, p.DID, types.EntityQuery{Type: "Goal"})
	if err != nil {
		return ps, fmt.Errorf("briefing: query goals: %w", err)
	}
	open := filter(goals, func(e *types.Entity) bool {
		s, _ := e.Data["status"].(string)
		return s == "" || s == "open"
	})
	ps.Sections = append(ps.Sections, Section{
		Title: fmt.Sprintf("Open goals (%d)", len(open)),
		Lines: goalLines(open),
	})

	// Recent observations (last 7 days)
	obs, _ := store.QueryEntities(ctx, p.DID, types.EntityQuery{Type: "Observation"})
	since := now.AddDate(0, 0, -7)
	recentObs := filter(obs, func(e *types.Entity) bool { return e.CreatedAt.After(since) })
	ps.Sections = append(ps.Sections, Section{
		Title: fmt.Sprintf("Recent observations (last 7 days, %d)", len(recentObs)),
		Lines: observationLines(recentObs),
	})

	// Stale entities — no updates >14 days, across all types.
	allTypes := []string{"Project", "Goal", "Decision", "Domain", "Observation"}
	staleCutoff := now.AddDate(0, 0, -14)
	var stale []*types.Entity
	for _, t := range allTypes {
		es, _ := store.QueryEntities(ctx, p.DID, types.EntityQuery{Type: t})
		for _, e := range es {
			if e.UpdatedAt.Before(staleCutoff) {
				stale = append(stale, e)
			}
		}
	}
	ps.Sections = append(ps.Sections, Section{
		Title: fmt.Sprintf("Stale entities (no updates >14 days, %d)", len(stale)),
		Lines: titleByTypeLines(stale),
	})

	// Active projects
	projects, _ := store.QueryEntities(ctx, p.DID, types.EntityQuery{Type: "Project"})
	active := filter(projects, func(e *types.Entity) bool {
		s, _ := e.Data["status"].(string)
		return s == "" || s == "active"
	})
	ps.Sections = append(ps.Sections, Section{
		Title: fmt.Sprintf("Active projects (%d)", len(active)),
		Lines: projectLines(active),
	})

	// Recent decisions (last 7 days)
	decisions, _ := store.QueryEntities(ctx, p.DID, types.EntityQuery{Type: "Decision"})
	recentDec := filter(decisions, func(e *types.Entity) bool { return e.CreatedAt.After(since) })
	ps.Sections = append(ps.Sections, Section{
		Title: fmt.Sprintf("Recent decisions (last 7 days, %d)", len(recentDec)),
		Lines: titleLines(recentDec),
	})

	if p.Name == CoggoPeerName {
		ps.Sections = append(ps.Sections, Section{
			Title: fmt.Sprintf("Open architectural decisions (%d)", len(decisions)),
			Lines: titleLines(decisions),
		})

		evs, _ := store.ListEvents(ctx, p.DID, since, now)
		count := 0
		for _, ev := range evs {
			if ev.Type == types.EventEntityCreated {
				count++
			}
		}
		ps.Sections = append(ps.Sections, Section{
			Title: fmt.Sprintf("Recent meaningful changes (last 7 days, %d)", count),
		})
	}

	return ps, nil
}

// ---- helpers ----

func filter(in []*types.Entity, keep func(*types.Entity) bool) []*types.Entity {
	out := make([]*types.Entity, 0, len(in))
	for _, e := range in {
		if keep(e) {
			out = append(out, e)
		}
	}
	return out
}

func excerpt(s string, n int) string {
	s = strings.TrimSpace(strings.SplitN(s, "\n", 2)[0])
	if len(s) > n {
		return s[:n-1] + "…"
	}
	return s
}

func dataString(e *types.Entity, key string) string {
	v, _ := e.Data[key].(string)
	return v
}

func goalLines(es []*types.Entity) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		title := dataString(e, "title")
		if t, ok := e.Data["target_date"].(string); ok && t != "" {
			out = append(out, fmt.Sprintf("%s (target %s)", title, t))
		} else if t, ok := e.Data["target_date"].(time.Time); ok && !t.IsZero() {
			out = append(out, fmt.Sprintf("%s (target %s)", title, t.Format(time.RFC3339)))
		} else {
			out = append(out, title)
		}
	}
	return out
}

func observationLines(es []*types.Entity) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, fmt.Sprintf("%q (%s)", excerpt(dataString(e, "text"), 80), e.CreatedAt.Format(time.RFC3339)))
	}
	return out
}

func projectLines(es []*types.Entity) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		title := dataString(e, "title")
		status := dataString(e, "status")
		if status == "" {
			status = "active"
		}
		var est string
		if v, ok := e.Data["completion_estimate"]; ok {
			est = fmt.Sprintf(" %v%%", v)
		}
		out = append(out, fmt.Sprintf("%s [%s%s]", title, status, est))
	}
	return out
}

func titleLines(es []*types.Entity) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, dataString(e, "title"))
	}
	return out
}

func titleByTypeLines(es []*types.Entity) []string {
	by := map[string][]string{}
	for _, e := range es {
		title := dataString(e, "title")
		if title == "" {
			title = excerpt(dataString(e, "text"), 60)
		}
		if title == "" {
			title = e.ID
		}
		by[e.Type] = append(by[e.Type], title)
	}
	keys := make([]string, 0, len(by))
	for k := range by {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(by))
	for _, k := range keys {
		out = append(out, fmt.Sprintf("%s: %s", k, strings.Join(by[k], ", ")))
	}
	return out
}

// Render formats a Briefing as the human-readable text shown for `coggo today`
// per spec §8.3.
func Render(b *Briefing) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Coggo daily briefing — %s\n", b.Date.Format("2006-01-02"))
	for _, ps := range b.Peers {
		fmt.Fprintf(&sb, "\n%s:\n", titleCase(ps.Name))
		for _, s := range ps.Sections {
			fmt.Fprintf(&sb, "  %s", s.Title)
			if len(s.Lines) == 0 {
				sb.WriteString("\n")
				continue
			}
			sb.WriteString(":\n")
			for _, line := range s.Lines {
				fmt.Fprintf(&sb, "    - %s\n", line)
			}
		}
	}
	sb.WriteString("\n(end of briefing)\n")
	return sb.String()
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
