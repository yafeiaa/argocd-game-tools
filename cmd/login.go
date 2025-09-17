package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/yovafeng/argocd-game-tools/internal/argocd"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "登录并验证与 Argo CD 的连接",
	RunE: func(cmd *cobra.Command, args []string) error {
		if serverAddr == "" {
			return fmt.Errorf("必须指定 --server 或设置 ARGOCD_SERVER")
		}

		fmt.Printf("[login] preparing client server=%s insecure=%v tlsNoVerify=%v user=%s token=%v\n",
			serverAddr, insecure, tlsNoVerify, username, authToken != "")

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		client, closer, err := argocd.NewClient(ctx, argocd.ClientConfig{
			ServerAddr:  serverAddr,
			Insecure:    insecure,
			TLSNoVerify: tlsNoVerify,
			Username:    username,
			Password:    password,
			AuthToken:   authToken,
			GRPCWeb:     grpcWeb,
			GRPCWebRoot: grpcWebRoot,
		})
		if err != nil {
			return err
		}
		defer closer()

		// 试探性调用：获取版本或项目列表确认连通
		ver, err := client.Version(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "[login] connected: %s\n", ver)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)
}
