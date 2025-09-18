package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/yafeiaa/argocd-game-tools/internal/argocd"
)

var appDownCmd = &cobra.Command{
	Use:   "down <name>",
	Short: "按 syncwave 逆序将应用内工作负载副本数置 0，并逐个等待",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		fmt.Printf("[down] preparing client server=%s insecure=%v tlsNoVerify=%v user=%s token=%v project=%s noGrace=%v grace=%d\n",
			serverAddr, insecure, tlsNoVerify, username, authToken != "", downProject, downNoGrace, downGracePeriod)

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

		fmt.Printf("[down] client ready, start app=%s project=%s noGrace=%v grace=%d\n", name, downProject, downNoGrace, downGracePeriod)
		return client.ScaleDownBySyncWave(ctx, downProject, name, downNoGrace, downGracePeriod)
	},
}

var (
	downProject     string
	downNoGrace     bool
	downGracePeriod int64
)
