package argocd

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	applications "github.com/argoproj/argo-cd/v2/pkg/apiclient/application"
	appv1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var (
	defaultPatchType = "application/merge-patch+json"
	defaultPatchJSON = `{"spec": {"replicas": 0}}`
)

// canScaleWorkloads 支持 scale 的工作负载
var canScaleWorkloads = map[string]struct{}{
	"Deployment":      {},
	"StatefulSet":     {},
	"GameDeployment":  {},
	"GameStatefulSet": {},
}

// getAppWorkloads 获取并按 syncWave 逆序排序
func (c *Client) getAppWorkloads(ctx context.Context, project, appName string) ([]appv1.ResourceStatus, error) {
	closer, appIf, err := c.conn.NewApplicationClient()
	if err != nil {
		return nil, err
	}
	defer closer.Close()
	app, err := appIf.Get(ctx, &applications.ApplicationQuery{
		Name:     &appName,
		Projects: []string{project},
	})
	if err != nil {
		return nil, err
	}
	var workloads []appv1.ResourceStatus
	for _, res := range app.Status.Resources {
		if _, ok := canScaleWorkloads[res.Kind]; ok {
			workloads = append(workloads, res)
		}
	}
	sort.Slice(workloads, func(i, j int) bool { return workloads[i].SyncWave > workloads[j].SyncWave })
	// logs: list workloads after sorting by SyncWave desc
	fmt.Printf("Found %d scalable workloads (sorted by SyncWave desc)\n", len(workloads))
	for _, r := range workloads {
		fmt.Printf("  wave=%d %s %s/%s\n", r.SyncWave, r.Kind, r.Namespace, r.Name)
	}
	return workloads, nil
}

// patchWorkloadReplicasZero 使用 PatchResource 将副本数设为 0
func (c *Client) patchWorkloadReplicasZero(ctx context.Context, project, appName string, r *appv1.ResourceStatus) error {
	closer, appIf, err := c.conn.NewApplicationClient()
	if err != nil {
		return err
	}
	defer closer.Close()
	// logs: before patch
	fmt.Printf("Patching replicas=0: %s %s/%s\n", r.Kind, r.Namespace, r.Name)
	_, err = appIf.PatchResource(ctx, &applications.ApplicationResourcePatchRequest{
		Name:         &appName,
		Project:      &project,
		ResourceName: &r.Name,
		Group:        &r.Group,
		Kind:         &r.Kind,
		Namespace:    &r.Namespace,
		Version:      &r.Version,
		PatchType:    &defaultPatchType,
		Patch:        &defaultPatchJSON,
	})
	if err != nil && !strings.Contains(err.Error(), "not found as part") {
		return err
	}
	// logs: after patch
	fmt.Printf("Patch sent: %s %s/%s\n", r.Kind, r.Namespace, r.Name)
	return nil
}

// waitPodsDeleted 等待该 workload 关联的 Pod 全部删除
func (c *Client) waitPodsDeleted(ctx context.Context, project, appName string, parent *appv1.ResourceStatus, noGrace bool, gracePeriod int64) error {
	closer, appIf, err := c.conn.NewApplicationClient()
	if err != nil {
		return err
	}
	defer closer.Close()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	firstLoop := true
	var k8sCli *kubernetes.Clientset
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			tree, err := appIf.ResourceTree(ctx, &applications.ResourcesQuery{
				Project:         &project,
				ApplicationName: &appName,
			})
			if err != nil {
				return err
			}
			parentNode := tree.FindNode(parent.Group, parent.Kind, parent.Namespace, parent.Name)
			if parentNode == nil {
				return nil
			}
			pods := 0
			var podNodes []appv1.ResourceNode
			for _, node := range tree.Nodes {
				if node.Kind != "Pod" {
					continue
				}
				// 仅统计该 workload 的子 Pod
				if isChildNode(tree, &node, parentNode) {
					// 排除 DaemonSet 生成的 Pod
					skip := false
					for _, p := range node.ParentRefs {
						if p.Kind == "DaemonSet" {
							skip = true
							break
						}
					}
					if !skip {
						pods++
						podNodes = append(podNodes, node)
					}
				}
			}
			if pods == 0 {
				fmt.Printf("All pods deleted for %s %s/%s\n", parent.Kind, parent.Namespace, parent.Name)
				return nil
			}
			fmt.Printf("Remaining pods=%d for %s %s/%s\n", pods, parent.Kind, parent.Namespace, parent.Name)

			// 强制删除（只在第一次循环执行一次）
			if noGrace && firstLoop {
				// 获取 app 以获取 kube-apiserver 地址
				app, err := appIf.Get(ctx, &applications.ApplicationQuery{Name: &appName, Projects: []string{project}})
				if err != nil {
					return err
				}
				if k8sCli == nil {
					token := c.conn.ClientOptions().AuthToken
					cli, err := newK8sClient(app.Spec.Destination.Server, token)
					if err != nil {
						return err
					}
					k8sCli = cli
				}
				gp := gracePeriod
				fmt.Printf("Force deleting %d pods (grace=%d) for %s %s/%s\n", len(podNodes), gp, parent.Kind, parent.Namespace, parent.Name)
				for _, p := range podNodes {
					// 忽略删除错误，除非不是 NotFound
					delErr := k8sCli.CoreV1().Pods(p.Namespace).Delete(ctx, p.Name, metav1.DeleteOptions{GracePeriodSeconds: &gp})
					if delErr != nil && !k8serrors.IsNotFound(delErr) {
						return fmt.Errorf("force delete pod %s/%s failed: %w", p.Namespace, p.Name, delErr)
					}
				}
				firstLoop = false
			}
		}
	}
}

// isChildNode 判断 node 是否为 parent 的子孙节点
func isChildNode(tree *appv1.ApplicationTree, node, parent *appv1.ResourceNode) bool {
	if node == nil || parent == nil {
		return false
	}
	if node.Name == parent.Name && node.Kind == parent.Kind && node.Namespace == parent.Namespace && node.Group == parent.Group {
		return true
	}
	for _, pr := range node.ParentRefs {
		if pr.Name == parent.Name && pr.Kind == parent.Kind && pr.Namespace == parent.Namespace && pr.Group == parent.Group {
			return true
		}
		pn := tree.FindNode(pr.Group, pr.Kind, pr.Namespace, pr.Name)
		if isChildNode(tree, pn, parent) {
			return true
		}
	}
	return false
}

// ScaleDownBySyncWave 将 app 内可缩容的 workload 按 syncWave 逆序置 0：
// - 同一 SyncWave 内并行 Patch 并等待其 Pod 删除
// - 不同 SyncWave 之间保持顺序，上一波完成后再进行下一波
func (c *Client) ScaleDownBySyncWave(ctx context.Context, project, appName string, noGrace bool, gracePeriod int64) error {
	fmt.Printf("Start scale down app=%s project=%s\n", appName, project)
	workloads, err := c.getAppWorkloads(ctx, project, appName)
	if err != nil {
		return err
	}
	// 将 workloads（已按 SyncWave 降序）分组
	var groups [][]appv1.ResourceStatus
	if len(workloads) > 0 {
		currentWave := workloads[0].SyncWave
		var buf []appv1.ResourceStatus
		for i := range workloads {
			w := workloads[i]
			if w.SyncWave != currentWave {
				// 推入上一组
				if len(buf) > 0 {
					groups = append(groups, buf)
				}
				buf = nil
				currentWave = w.SyncWave
			}
			buf = append(buf, w)
		}
		if len(buf) > 0 {
			groups = append(groups, buf)
		}
	}

	// 按波次执行：同波并行，波次之间串行
	for _, group := range groups {
		if len(group) == 0 {
			continue
		}
		wave := group[0].SyncWave
		fmt.Printf("Processing wave=%d with %d workloads in parallel\n", wave, len(group))
		g, gctx := errgroup.WithContext(ctx)
		for i := range group {
			w := group[i]
			wCopy := w
			g.Go(func() error {
				if err := c.patchWorkloadReplicasZero(gctx, project, appName, &wCopy); err != nil {
					return fmt.Errorf("patch %s/%s/%s replicas=0: %w", wCopy.Kind, wCopy.Namespace, wCopy.Name, err)
				}
				if err := c.waitPodsDeleted(gctx, project, appName, &wCopy, noGrace, gracePeriod); err != nil {
					return fmt.Errorf("wait pods deleted for %s/%s/%s: %w", wCopy.Kind, wCopy.Namespace, wCopy.Name, err)
				}
				fmt.Printf("Scaled down: %s %s/%s\n", wCopy.Kind, wCopy.Namespace, wCopy.Name)
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			return err
		}
		fmt.Printf("Wave %d completed\n", wave)
	}
	fmt.Println("Scale down finished")
	return nil
}

func newK8sClient(server, token string) (*kubernetes.Clientset, error) {
	cfg := &rest.Config{
		Host:            server,
		BearerToken:     token,
		TLSClientConfig: rest.TLSClientConfig{Insecure: true},
	}
	return kubernetes.NewForConfig(cfg)
}
