package certificate

import (
	core "dasslm/server/internal/handler"
	"net/http"
	"time"

	"dasslm/server/internal/model"
	deployservice "dasslm/server/internal/service/deploy"
	proxyservice "dasslm/server/internal/service/proxypool"

	"gorm.io/gorm"
)

type deployRequestTrace = deployservice.Trace

func deployHTTPClient(db *gorm.DB, account model.DeployAccount) (*http.Client, error) {
	proxySetting, err := core.DeployAccountProxy(db, account)
	if err != nil {
		return nil, err
	}
	return proxyservice.Client(proxySetting, 30*time.Second)
}
