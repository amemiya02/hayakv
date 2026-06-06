package iface_test

import (
	"testing"

	database "github.com/amemiya02/hayakv/internal/command"
	"github.com/amemiya02/hayakv/internal/iface"
	idatabase "github.com/amemiya02/hayakv/internal/iface/database"
	goroutinenet "github.com/amemiya02/hayakv/internal/net/goroutine"
	"github.com/amemiya02/hayakv/internal/proto/resp2"
)

func TestGodisBaselineImplementsM0Seams(t *testing.T) {
	var _ iface.StorageEngine = (*database.Server)(nil)
	var _ iface.NetHandler = (*goroutinenet.Handler)(nil)
	var _ iface.NetServer = (*goroutinenet.Server)(nil)
	var _ iface.ProtocolCodec = resp2.Codec{}
	var _ iface.Object = (*idatabase.DataEntity)(nil)
}
