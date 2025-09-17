package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	appapi "github.com/argoproj/argo-cd/v2/pkg/apiclient/application"
	"github.com/yovafeng/argocd-game-tools/internal/argocd"
)

var appCmd = &cobra.Command{
	Use:   "app",
	Short: "应用相关操作",
}

var appListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出应用",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		client, closer, err := argocd.NewClient(ctx, argocd.ClientConfig{
			ServerAddr:  serverAddr,
			Insecure:    insecure,
			TLSNoVerify: tlsNoVerify,
			Username:    username,
			Password:    password,
			AuthToken:   authToken,
		})
		if err != nil {
			return err
		}
		defer closer()
		list, err := client.ListApplications(ctx, &appapi.ApplicationQuery{})
		if err != nil {
			return err
		}
		for _, a := range list.Items {
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", a.Name, a.Status.Sync.Status, a.Status.Health.Status)
		}
		return nil
	},
}

var appGetCmd = &cobra.Command{
	Use:   "get <name>",
	Short: "获取应用详情",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		client, closer, err := argocd.NewClient(ctx, argocd.ClientConfig{
			ServerAddr:  serverAddr,
			Insecure:    insecure,
			TLSNoVerify: tlsNoVerify,
			Username:    username,
			Password:    password,
			AuthToken:   authToken,
		})
		if err != nil {
			return err
		}
		defer closer()
		app, err := client.GetApplication(ctx, name)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s\nSync: %s\nHealth: %s\n", app.Name, app.Status.Sync.Status, app.Status.Health.Status)
		return nil
	},
}

var (
	flagPrune  bool
	flagDryRun bool
	flagWait   time.Duration
)

var appSyncCmd = &cobra.Command{
	Use:   "sync <name>",
	Short: "同步应用",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		client, closer, err := argocd.NewClient(ctx, argocd.ClientConfig{
			ServerAddr:  serverAddr,
			Insecure:    insecure,
			TLSNoVerify: tlsNoVerify,
			Username:    username,
			Password:    password,
			AuthToken:   authToken,
		})
		if err != nil {
			return err
		}
		defer closer()
		_, err = client.SyncApplication(ctx, name, flagPrune, flagDryRun, nil)
		if err != nil {
			return err
		}
		if flagWait > 0 {
			wctx, cancel := context.WithTimeout(context.Background(), flagWait)
			defer cancel()
			if err := client.WaitForHealthy(wctx, name, flagWait); err != nil {
				return err
			}
		}
		fmt.Fprintln(cmd.OutOrStdout(), "sync requested")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(appCmd)
	appCmd.AddCommand(appListCmd)
	appCmd.AddCommand(appGetCmd)
	appCmd.AddCommand(appSyncCmd)
	appCmd.AddCommand(appDownCmd)

	appSyncCmd.Flags().BoolVar(&flagPrune, "prune", false, "允许删除不在期望状态的资源")
	appSyncCmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "仅试运行")
	appSyncCmd.Flags().DurationVar(&flagWait, "wait", 0, "同步后等待健康的时间 (例如 60s)")

	// down flags
	appDownCmd.Flags().StringVar(&downProject, "project", "", "所属项目（用于资源过滤与权限校验）")
	appDownCmd.Flags().BoolVar(&downNoGrace, "no-grace", false, "强制删除 Pod（立即或指定宽限期）")
	appDownCmd.Flags().Int64Var(&downGracePeriod, "grace-period", 0, "Pod 删除宽限期秒数（与 --no-grace 联合使用）")
}
