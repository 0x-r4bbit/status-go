package wakuext

import (
	"github.com/syndtr/goleveldb/leveldb"

	"github.com/ethereum/go-ethereum/rpc"

	"github.com/status-im/status-go/eth-node/types"
	"github.com/status-im/status-go/params"
	"github.com/status-im/status-go/services/ext"
)

type Service struct {
	*ext.Service
	w types.Waku
}

func New(config params.NodeConfig, n types.Node, handler ext.EnvelopeEventsHandler, ldb *leveldb.DB) *Service {
	w, err := n.GetWaku(nil)
	if err != nil {
		panic(err)
	}
	delay := ext.DefaultRequestsDelay
	if config.ShhextConfig.RequestsDelay != 0 {
		delay = config.ShhextConfig.RequestsDelay
	}
	requestsRegistry := ext.NewRequestsRegistry(delay)
	mailMonitor := ext.NewMailRequestMonitor(w, handler, requestsRegistry)
	return &Service{
		Service: ext.New(config, n, ldb, mailMonitor, w),
		w:       w,
	}
}

func (s *Service) PublicWakuAPI() types.PublicWakuAPI {
	return s.w.PublicWakuAPI()
}

// APIs returns a list of new APIs.
func (s *Service) APIs() []rpc.API {
	apis := []rpc.API{
		{
			Namespace: "wakuext",
			Version:   "1.0",
			Service:   NewPublicAPI(s),
			Public:    false,
		},
	}
	return apis
}
