package certificate

import (
	core "dasslm/server/internal/handler"
	"net/http"
	"time"

	"dasslm/server/internal/model"
	deployservice "dasslm/server/internal/service/deploy"

	"gorm.io/gorm"
)

type deployRequestTrace = deployservice.Trace

// deployHTTPClient 与部署账号测试、站点列表使用同一套客户端（含代理与宝塔面板自签名证书处理），
// 否则测试连接正常而实际部署会因证书校验失败。
func deployHTTPClient(db *gorm.DB, account model.DeployAccount) (*http.Client, error) {
	return core.DeployHTTPClient(db, account, 30*time.Second)
}
