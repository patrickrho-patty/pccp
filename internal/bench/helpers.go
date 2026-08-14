package bench

import (
	"bytes"
	"net"
	"os"

	"github.com/fxamacker/cbor/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
)

// openBenchDB opens an isolated sqlite database for a bench run.
func openBenchDB(path string) (*gorm.DB, error) {
	g, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		return nil, err
	}
	if err := g.AutoMigrate(models.AllModels()...); err != nil {
		return nil, err
	}
	return g, nil
}

// identityEnrollRequest builds the enroll payload.
func identityEnrollRequest(orgID, harnessID, pubHex string) identity.EnrollHarnessRequest {
	return identity.EnrollHarnessRequest{
		OrganizationID: orgID,
		HarnessID:      harnessID,
		PublicKeyHex:   pubHex,
		BinaryVersion:  "bench",
		BinaryHash:     "bench",
	}
}

// bytesReader keeps the SSE arm self-contained.
func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }

// paperUnmarshal decodes CBOR (fallback path in the bench client).
func paperUnmarshal(data []byte, v interface{}) error { return cbor.Unmarshal(data, v) }

// osGetenv/osSetenv keep env manipulation stubbable.
func osGetenv(k string) string { return os.Getenv(k) }
func osSetenv(k, v string)     { os.Setenv(k, v) }

// freeLocalAddr returns a free 127.0.0.1 port as an address string.
func freeLocalAddr() string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "127.0.0.1:18444"
	}
	defer ln.Close()
	return ln.Addr().String()
}
