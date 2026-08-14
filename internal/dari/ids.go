package dari

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// GenerateID generates a time-prefixed unique identifier.
// Format: prefix_<hex_seconds>_<random_hex>
// This produces IDs that are time-sortable while remaining unpredictable.
func GenerateID(prefix string) string {
	ts := time.Now().UnixMilli()
	randBytes := make([]byte, 10)
	rand.Read(randBytes)
	return fmt.Sprintf("%s_%013d%s", prefix, ts, hex.EncodeToString(randBytes))
}
