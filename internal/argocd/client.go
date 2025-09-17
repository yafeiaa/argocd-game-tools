package argocd

import (
	"context"
	"fmt"
	"strings"
	"time"

	apiclient "github.com/argoproj/argo-cd/v2/pkg/apiclient"
	applications "github.com/argoproj/argo-cd/v2/pkg/apiclient/application"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient/session"
	appv1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/argoproj/gitops-engine/pkg/health"
)

// ClientConfig 定义连接 Argo CD 的参数
type ClientConfig struct {
	ServerAddr  string
	Insecure    bool
	TLSNoVerify bool
	Username    string
	Password    string
	AuthToken   string
	GRPCWeb     bool
	GRPCWebRoot string
}

// Client 封装对各服务客户端的访问
type Client struct {
	conn apiclient.Client
}

// NewClient 创建 Argo CD API 客户端
func NewClient(ctx context.Context, cfg ClientConfig) (*Client, func(), error) {
	if cfg.ServerAddr == "" {
		return nil, nil, fmt.Errorf("ServerAddr 不能为空")
	}

	fmt.Printf("[client] init server=%s insecure=%v tlsNoVerify=%v hasToken=%v user=%s\n",
		cfg.ServerAddr, cfg.Insecure, cfg.TLSNoVerify, cfg.AuthToken != "", cfg.Username)

	// 注意：PlainText 仅在明确需要明文 gRPC 时才应开启
	// 这里默认走 TLS，--tls-no-verify 控制证书校验，避免把 --insecure 误当作明文连接
	clientOpts := apiclient.ClientOptions{
		ServerAddr:      cfg.ServerAddr,
		Insecure:        cfg.TLSNoVerify,
		PlainText:       cfg.Insecure,
		AuthToken:       cfg.AuthToken,
		GRPCWeb:         cfg.GRPCWeb,
		GRPCWebRootPath: cfg.GRPCWebRoot,
	}

	client, err := apiclient.NewClient(&clientOpts)
	if err != nil {
		return nil, nil, err
	}

	// 若无 token 且提供用户名密码，则通过 Session.Create 登录获取 token 并重建 client
	if clientOpts.AuthToken == "" && cfg.Username != "" {
		fmt.Printf("[client] no token, trying session login with username=%s\n", cfg.Username)
		closer, sessIf, err := client.NewSessionClient()
		if err != nil {
			return nil, nil, err
		}
		defer func() { _ = closer.Close() }()
		resp, err := sessIf.Create(ctx, &session.SessionCreateRequest{Username: cfg.Username, Password: cfg.Password})
		if err != nil {
			// 若为证书校验错误，自动回退为 Insecure TLS 再试一次
			if strings.Contains(err.Error(), "x509:") || strings.Contains(err.Error(), "certificate signed by unknown authority") {
				fmt.Printf("[client] session login failed due to TLS verify, retry with tls-no-verify\n")
				clientOpts.Insecure = true
				client, err = apiclient.NewClient(&clientOpts)
				if err != nil {
					return nil, nil, err
				}
				closer, sessIf, err = client.NewSessionClient()
				if err != nil {
					return nil, nil, err
				}
				defer func() { _ = closer.Close() }()
				resp, err = sessIf.Create(ctx, &session.SessionCreateRequest{Username: cfg.Username, Password: cfg.Password})
				if err != nil {
					return nil, nil, err
				}
			} else {
				return nil, nil, err
			}
		}
		if resp != nil && resp.Token != "" {
			fmt.Printf("[client] session login success, got token\n")
			clientOpts.AuthToken = resp.Token
			client, err = apiclient.NewClient(&clientOpts)
			if err != nil {
				return nil, nil, err
			}
		}
	}

	// apiclient.Client 自身不暴露 Close 方法，返回一个空 closer
	return &Client{conn: client}, func() {}, nil
}

// Version 读取服务器版本（通过 application 客户端的 List 接口探测）
func (c *Client) Version(ctx context.Context) (string, error) {
	// 使用 Application.List 轻探测
	closer, appIf, err := c.conn.NewApplicationClient()
	if err != nil {
		return "", err
	}
	defer func() { _ = closer.Close() }()
	_, err = appIf.List(ctx, &applications.ApplicationQuery{})
	if err != nil {
		return "", err
	}
	return "ok", nil
}

// ListApplications 返回应用列表
func (c *Client) ListApplications(ctx context.Context, query *applications.ApplicationQuery) (*appv1.ApplicationList, error) {
	closer, appIf, err := c.conn.NewApplicationClient()
	if err != nil {
		return nil, err
	}
	defer func() { _ = closer.Close() }()
	return appIf.List(ctx, query)
}

// GetApplication 获取单个应用
func (c *Client) GetApplication(ctx context.Context, name string) (*appv1.Application, error) {
	closer, appIf, err := c.conn.NewApplicationClient()
	if err != nil {
		return nil, err
	}
	defer func() { _ = closer.Close() }()
	q := &applications.ApplicationQuery{}
	q.Name = &name
	return appIf.Get(ctx, q)
}

// SyncApplication 触发同步
func (c *Client) SyncApplication(ctx context.Context, name string, prune bool, dryRun bool, strategy *appv1.SyncStrategy) (*appv1.Application, error) {
	closer, appIf, err := c.conn.NewApplicationClient()
	if err != nil {
		return nil, err
	}
	defer func() { _ = closer.Close() }()
	qName := name
	dr := dryRun
	pr := prune
	req := &applications.ApplicationSyncRequest{
		Name:     &qName,
		DryRun:   &dr,
		Prune:    &pr,
		Strategy: strategy,
	}
	return appIf.Sync(ctx, req)
}

// WaitForHealthy 等待应用健康并同步完成
func (c *Client) WaitForHealthy(ctx context.Context, name string, timeout time.Duration) error {
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	ch := c.conn.WatchApplicationWithRetry(watchCtx, name, "")
	deadline := time.Now().Add(timeout)
	for ev := range ch {
		if ev == nil {
			continue
		}
		app := ev.Application
		// app 是值类型，无法与 nil 比较；检查名称是否为空来过滤无效事件
		if app.Name == "" {
			continue
		}
		if app.Status.Sync.Status == appv1.SyncStatusCodeSynced && app.Status.Health.Status == health.HealthStatusHealthy {
			return nil
		}
		if timeout > 0 && time.Now().After(deadline) {
			return fmt.Errorf("wait healthy timeout: %s", name)
		}
	}
	return fmt.Errorf("watch closed before healthy: %s", name)
}
