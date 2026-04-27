package peer

import (
	"encoding/base64"
	"sort"

	"github.com/lunguini/coggo/internal/types"
)

func base64StdEncode(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func base64StdDecode(s string) []byte {
	b, _ := base64.StdEncoding.DecodeString(s)
	return b
}

func sortByName(ps []*types.Peer) {
	sort.Slice(ps, func(i, j int) bool { return ps[i].Name < ps[j].Name })
}
