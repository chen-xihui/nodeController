package node

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// NodeManager handles node-related operations
type NodeManager struct {
	Clientset         *kubernetes.Clientset
	ResourceThreshold float64
	EnableMultiAZ     bool
}

// NewNodeManager creates a new NodeManager
func NewNodeManager(clientset *kubernetes.Clientset, resourceThreshold float64, enableMultiAZ bool) *NodeManager {
	return &NodeManager{
		Clientset:         clientset,
		ResourceThreshold: resourceThreshold,
		EnableMultiAZ:     enableMultiAZ,
	}
}

// CheckNodeStatus checks node status and handles failover
func (nm *NodeManager) CheckNodeStatus() {
	nodes, err := nm.Clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		fmt.Printf("  ERROR: Failed to list nodes: %v\n", err)
		return
	}

	for _, node := range nodes.Items {
		if val, ok := node.Labels["node-role.kubernetes.io/role"]; ok && val == "primary" {
			nm.checkPrimaryNodeStatus(node)
		}
	}
}

// checkPrimaryNodeStatus checks primary node status and handles failures
func (nm *NodeManager) checkPrimaryNodeStatus(node corev1.Node) {
	// Check if node is ready
	isReady := false
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
			isReady = true
			break
		}
	}

	if !isReady {
		fmt.Printf("  WARNING: Primary node %s is not ready\n", node.Name)
		// Handle node failure
		nm.handleNodeFailure(node)
		return
	}

	// Check resource usage
	resourceUsage, err := nm.calculateResourceUsage(node)
	if err != nil {
		fmt.Printf("  ERROR: Failed to calculate resource usage for node %s: %v\n", node.Name, err)
		return
	}

	if resourceUsage > nm.ResourceThreshold {
		fmt.Printf("  WARNING: Primary node %s resource usage is high: %.2f%%\n", node.Name, resourceUsage)
		nm.markNodeAsOverloaded(node.Name)
	} else {
		nm.removeNodeOverloadTaint(node.Name)
	}
}

// calculateResourceUsage calculates node resource usage percentage
func (nm *NodeManager) calculateResourceUsage(node corev1.Node) (float64, error) {
	// Calculate CPU usage
	var cpuUsage float64
	var memoryUsage float64
	// 注意修改
	// This is a simplified calculation, actual implementation would use metrics-server
	// For demonstration purposes, we'll return a dummy value
	return 50.0, nil
}

// handleNodeFailure handles node failure and triggers failover
func (nm *NodeManager) handleNodeFailure(node corev1.Node) {
	// Mark node as failed
	cmd := exec.Command("kubectl", "label", "nodes", node.Name, "node-role.kubernetes.io/failed=true", "--overwrite")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("  ERROR: Failed to mark node %s as failed: %s\n", node.Name, string(output))
	} else {
		fmt.Printf("  SUCCESS: Node %s has been marked as failed\n", node.Name)
	}

	// Check if there are other available primary nodes
	availablePrimaryNodes := nm.countAvailablePrimaryNodes()
	if availablePrimaryNodes == 0 {
		fmt.Printf("  CRITICAL: No primary nodes available! Promoting backup nodes...\n")
		nm.PromoteBackupNodes()
	} else {
		// Check if there are available primary nodes in the same zone
		availablePrimaryNodesInZone := 0
		if nm.EnableMultiAZ {
			nodeZone := getNodeZone(node)
			if nodeZone != "" {
				availablePrimaryNodesInZone = nm.countAvailablePrimaryNodesInZone(nodeZone)
				fmt.Printf("  INFO: Found %d available primary nodes in zone %s\n", availablePrimaryNodesInZone, nodeZone)
			}
		}

		if availablePrimaryNodesInZone == 0 {
			fmt.Printf("  CRITICAL: No primary nodes available in zone! Promoting backup nodes...\n")
			nm.PromoteBackupNodes()
		} else {
			fmt.Printf("  INFO: Found %d available primary nodes, no need to promote backup nodes\n", availablePrimaryNodes)
		}
	}
}

// countAvailablePrimaryNodes counts available primary nodes
func (nm *NodeManager) countAvailablePrimaryNodes() int {
	nodes, err := nm.Clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return 0
	}

	count := 0
	for _, node := range nodes.Items {
		if val, ok := node.Labels["node-role.kubernetes.io/role"]; ok && val == "primary" {
			isReady := false
			for _, condition := range node.Status.Conditions {
				if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
					isReady = true
					break
				}
			}
			if isReady {
				count++
			}
		}
	}

	return count
}

// countAvailablePrimaryNodesInZone counts available primary nodes in a specific zone
func (nm *NodeManager) countAvailablePrimaryNodesInZone(zone string) int {
	nodes, err := nm.Clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return 0
	}

	count := 0
	for _, node := range nodes.Items {
		// Check if node is in the specified zone
		if nodeZone := getNodeZone(node); nodeZone != zone {
			continue
		}

		// Check if node is a primary node
		if val, ok := node.Labels["node-role.kubernetes.io/role"]; ok && val == "primary" {
			// Check if node is ready
			isReady := false
			for _, condition := range node.Status.Conditions {
				if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
					isReady = true
					break
				}
			}
			if isReady {
				count++
			}
		}
	}

	return count
}

// PromoteBackupNodes promotes backup nodes to primary
func (nm *NodeManager) PromoteBackupNodes() {
	// Implementation will be added here
}

// DemoteNodeToBackup demotes a node to backup
func (nm *NodeManager) DemoteNodeToBackup(nodeName string) {
	// Remove primary label and promoted label
	cmd := exec.Command("kubectl", "label", "nodes", nodeName, "node-role.kubernetes.io/role-", "node-role.kubernetes.io/promoted-", "node-role.kubernetes.io/failed-")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("  ERROR: Failed to demote node %s: %s\n", nodeName, string(output))
	} else {
		fmt.Printf("  SUCCESS: Node %s has been demoted to backup\n", nodeName)
	}
}

// markNodeAsOverloaded marks a node as overloaded
func (nm *NodeManager) markNodeAsOverloaded(nodeName string) {
	// Add NoSchedule taint to prevent new pods from being scheduled
	cmd := exec.Command("kubectl", "taint", "nodes", nodeName, "node-role.kubernetes.io/overloaded:NoSchedule", "--overwrite")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("  ERROR: Failed to mark node %s as overloaded: %s\n", nodeName, string(output))
	} else {
		fmt.Printf("  SUCCESS: Node %s has been marked as overloaded\n", nodeName)
	}
}

// removeNodeOverloadTaint removes node overload taint
func (nm *NodeManager) removeNodeOverloadTaint(nodeName string) {
	// Remove overloaded taint
	cmd := exec.Command("kubectl", "taint", "nodes", nodeName, "node-role.kubernetes.io/overloaded:")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Ignore error if taint doesn't exist
		if !strings.Contains(string(output), "not found") {
			fmt.Printf("  ERROR: Failed to remove overload taint from node %s: %s\n", nodeName, string(output))
		}
	} else {
		fmt.Printf("  SUCCESS: Removed overload taint from node %s\n", nodeName)
	}
}

// getNodeZone gets the zone of a node
func getNodeZone(node corev1.Node) string {
	if zone, ok := node.Labels["topology.kubernetes.io/zone"]; ok {
		return zone
	}
	return ""
}
