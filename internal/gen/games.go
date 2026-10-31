package gen

import (
	"sort"

	"foxgozar/internal/state"
)

// GamePorts are the UDP ports a game's own match traffic uses, by game id
// (see app.Games). Only games whose ports are well known and stable are
// here; a game without an entry cannot be given a path of its own.
var GamePorts = map[string][]string{
	"dota2":    {"27015:27200"},            // Valve, incl. Steam Datagram Relay
	"cs2":      {"27015:27200"},            // Valve
	"lol":      {"5000:5500"},              // Riot: League of Legends
	"valorant": {"7000:8000", "8180:8181"}, // Riot: Valorant
	"fortnite": {"9000:9100"},              // Epic
}

// GameDirect as a game's route: its traffic leaves the core directly.
const GameDirect = "direct"

// gameRules route each game that has a path of its own ("direct" or a
// server id) by its UDP ports. A server that no longer exists is skipped:
// the game then follows the tunnel like everything else.
func gameRules(st state.State) []any {
	ids := make([]string, 0, len(st.GameRoutes))
	for id := range st.GameRoutes {
		ids = append(ids, id)
	}
	sort.Strings(ids) // the same config for the same choices
	var rules []any
	for _, id := range ids {
		via, ports := st.GameRoutes[id], GamePorts[id]
		if via == "" || len(ports) == 0 {
			continue
		}
		out := via
		if via == GameDirect {
			out = TagDirect
		} else if st.NodeByID(via) == nil {
			continue
		}
		pr := make([]any, len(ports))
		for i, p := range ports {
			pr[i] = p
		}
		rules = append(rules, map[string]any{"network": []any{"udp"}, "port_range": pr, "outbound": out})
	}
	return rules
}
