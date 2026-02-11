package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

var (
	kubeconfig              *string
	autoMode                bool
	interval                time.Duration
	primaryNodes            []string
	backupNodes             []string
	resourceThreshold       float64
	initialBackupNodesCount int
	initPrimaryNodesCount   int
	enableMultiAZ           bool
	zoneLabels              []string
)

func init() {
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.BoolVar(&autoMode, "auto", false, "enable automatic mode")
	flag.DurationVar(&interval, "interval", 30*time.Second, "check interval in automatic mode")
	flag.Float64Var(&resourceThreshold, "threshold", 80.0, "resource usage threshold percentage")
	flag.BoolVar(&enableMultiAZ, "multiaz", false, "enable multi-availability zone support")
	flag.Parse()

	if enableMultiAZ {
		zoneLabels = []string{
			"topology.kubernetes.io/zone=region-name.az01arm",
			"topology.kubernetes.io/zone=region-name.az02arm",
			"topology.kubernetes.io/zone=region-name.az03arm",
		}
	}
}

func main() {
	if len(os.Args) < 2 || os.Args[1] == "--help" || os.Args[1] == "-h" {
		printUsage()
		return
	}

	subcommand := os.Args[1]
	switch subcommand {
	case "auto":
		handleAutoCommand()
	case "label":
		handleLabelCommand()
	case "taint":
		handleTaintCommand()
	case "status":
		handleStatusCommand()
	case "monitor":
		handleMonitorCommand()
	case "help":
		printUsage()
	default:
		fmt.Printf("Unknown command: %s\n", subcommand)
		printUsage()
	}
}

func printUsage() {
	fmt.Println("Node Controller Tool")
	fmt.Println("Usage:")
	fmt.Println("  nodecontroller auto [--kubeconfig=<path>] [--interval=<duration>] [--threshold=<percentage>] [--multiaz]")
	fmt.Println("  nodecontroller label <nodes> --role=<primary|backup>")
	fmt.Println("  nodecontroller taint <nodes> --role=<primary|backup>")
	fmt.Println("  nodecontroller status [nodes]")
	fmt.Println("  nodecontroller monitor [--interval=<duration>]")
	fmt.Println("  nodecontroller help")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  --multiaz              Enable multi-availability zone support")
	fmt.Println("  --interval=<duration>  Check interval in automatic mode (default: 30s)")
	fmt.Println("  --threshold=<percentage> Resource usage threshold percentage (default: 80)")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  nodecontroller auto --interval=1m --threshold=75")
	fmt.Println("  nodecontroller auto --multiaz --interval=1m --threshold=75")
	fmt.Println("  nodecontroller label node1 node2 --role=primary")
	fmt.Println("  nodecontroller label node3 node4 --role=backup")
	fmt.Println("  nodecontroller taint node1 node2 --role=primary")
	fmt.Println("  nodecontroller taint node3 node4 --role=backup")
	fmt.Println("  nodecontroller status")
	fmt.Println("  nodecontroller status node1 node2")
	fmt.Println("  nodecontroller monitor --interval=30s")
}

func handleAutoCommand() {
	fmt.Println("Starting automatic node controller...")
	fmt.Printf("Check interval: %v\n", interval)
	fmt.Printf("Resource threshold: %.1f%%\n", resourceThreshold)
	if enableMultiAZ {
		fmt.Println("Multi-AZ support: ENABLED")
		fmt.Printf("Zone labels: %v\n", zoneLabels)
	} else {
		fmt.Println("Multi-AZ support: DISABLED")
	}

	for {
		fmt.Println("\n--- Checking node status ---")
		checkAndUpdateNodeStatus()
		fmt.Printf("Sleeping for %v...\n", interval)
		time.Sleep(interval)
	}
}

func handleLabelCommand() {
	if len(os.Args) < 4 {
		fmt.Println("Error: Missing required arguments")
		fmt.Println("Usage: nodecontroller label <nodes> --role=<primary|backup>")
		return
	}

	nodes := []string{}
	role := ""

	for i := 2; i < len(os.Args); i++ {
		arg := os.Args[i]
		if strings.HasPrefix(arg, "--role=") {
			role = strings.TrimPrefix(arg, "--role=")
		} else {
			nodes = append(nodes, arg)
		}
	}

	if role != "primary" && role != "backup" {
		fmt.Println("Error: Invalid role. Must be 'primary' or 'backup'")
		return
	}

	if len(nodes) == 0 {
		fmt.Println("Error: No nodes specified")
		return
	}

	for _, node := range nodes {
		labelNode(node, role)
	}
}

func handleTaintCommand() {
	if len(os.Args) < 4 {
		fmt.Println("Error: Missing required arguments")
		fmt.Println("Usage: nodecontroller taint <nodes> --role=<primary|backup>")
		return
	}

	nodes := []string{}
	role := ""

	for i := 2; i < len(os.Args); i++ {
		arg := os.Args[i]
		if strings.HasPrefix(arg, "--role=") {
			role = strings.TrimPrefix(arg, "--role=")
		} else {
			nodes = append(nodes, arg)
		}
	}

	if role != "primary" && role != "backup" {
		fmt.Println("Error: Invalid role. Must be 'primary' or 'backup'")
		return
	}

	if len(nodes) == 0 {
		fmt.Println("Error: No nodes specified")
		return
	}

	for _, node := range nodes {
		taintNode(node, role)
	}
}

func handleStatusCommand() {
	nodes := []string{}
	if len(os.Args) > 2 {
		nodes = os.Args[2:]
	}

	printNodeStatus(nodes)
}

func handleMonitorCommand() {
	fmt.Println("Starting node monitor...")
	fmt.Printf("Check interval: %v\n", interval)

	for {
		fmt.Println("\n--- Monitoring node status ---")
		printNodeStatus([]string{})
		fmt.Printf("Sleeping for %v...\n", interval)
		time.Sleep(interval)
	}
}

func labelNode(node, role string) {
	// Remove existing role labels
	exec.Command("kubectl", "label", "nodes", node, "node-role.kubernetes.io/primary-", "node-role.kubernetes.io/backup-", "node-role.kubernetes.io/role-", "node-role.kubernetes.io/promoted-", "--overwrite").Run()

	// Add new role label
	if role == "primary" {
		cmd := exec.Command("kubectl", "label", "nodes", node, "node-role.kubernetes.io/role=primary", "--overwrite")
		output, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Printf("Error labeling node %s: %s\n", node, string(output))
		} else {
			fmt.Printf("Successfully labeled node %s as primary\n", node)
		}
	} else if role == "backup" {
		// Backup nodes have no labels in this scheme
		fmt.Printf("Successfully configured node %s as backup (no labels)\n", node)
	}
}

func taintNode(node, role string) {
	// Remove existing taints - no longer needed in this scheme
	exec.Command("kubectl", "taint", "nodes", node, "node-role.kubernetes.io/primary:", "-", "node-role.kubernetes.io/backup:", "-", "node-role.kubernetes.io/role:", "-").Run()

	if role == "primary" {
		fmt.Printf("Successfully configured node %s as primary (no taints needed)\n", node)
	} else if role == "backup" {
		fmt.Printf("Successfully configured node %s as backup (no taints needed)\n", node)
	}
}

func printNodeStatus(nodes []string) {
	var cmd *exec.Cmd
	if len(nodes) > 0 {
		cmd = exec.Command("kubectl", append([]string{"get", "nodes", "-o", "wide"}, nodes...)...)
	} else {
		cmd = exec.Command("kubectl", "get", "nodes", "-o", "wide")
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("Error getting node status: %s\n", string(output))
		return
	}

	fmt.Println("Node Status:")
	fmt.Println(string(output))

	// Get detailed labels and taints
	fmt.Println("\nDetailed Node Information:")
	if len(nodes) > 0 {
		for _, node := range nodes {
			printNodeDetails(node)
		}
	} else {
		// Get all nodes
		cmd := exec.Command("kubectl", "get", "nodes", "-o", "name")
		output, err := cmd.CombinedOutput()
		if err == nil {
			nodeNames := strings.Split(strings.TrimSpace(string(output)), "\n")
			for _, nodeName := range nodeNames {
				node := strings.TrimPrefix(nodeName, "node/")
				if node != "" {
					printNodeDetails(node)
				}
			}
		}
	}
}

func printNodeDetails(node string) {
	fmt.Printf("\nNode: %s\n", node)
	fmt.Println("Labels:")
	cmd := exec.Command("kubectl", "get", "node", node, "-o", "jsonpath={.metadata.labels}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("  Error: %s\n", string(output))
	} else {
		fmt.Printf("  %s\n", string(output))
	}

	fmt.Println("Taints:")
	cmd = exec.Command("kubectl", "get", "node", node, "-o", "jsonpath={.spec.taints}")
	output, err = cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("  Error: %s\n", string(output))
	} else {
		if string(output) == "" {
			fmt.Println("  <none>")
		} else {
			fmt.Printf("  %s\n", string(output))
		}
	}

	fmt.Println("Resource Usage:")
	cmd = exec.Command("kubectl", "top", "node", node)
	output, err = cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("  Error: %s\n", string(output))
	} else {
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		for i, line := range lines {
			if i > 0 {
				fmt.Printf("  %s\n", line)
			}
		}
	}
}

func checkAndUpdateNodeStatus() {
	// Get kubernetes client
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		log.Printf("Error building kubeconfig: %v", err)
		return
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Printf("Error creating kubernetes client: %v", err)
		return
	}

	// Get all nodes
	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		log.Printf("Error listing nodes: %v", err)
		return
	}

	primaryNodes := []corev1.Node{}
	backupNodes := []corev1.Node{}
	unlabeledNodes := []corev1.Node{}

	// Classify nodes
	for _, node := range nodes.Items {
		if _, ok := node.Labels["node-role.kubernetes.io/role"]; ok {
			if node.Labels["node-role.kubernetes.io/role"] == "primary" {
				primaryNodes = append(primaryNodes, node)
			}
		} else {
			backupNodes = append(backupNodes, node)
		}
	}

	// Unlabeled nodes are considered backup nodes in this scheme
	unlabeledNodes = []corev1.Node{} // No unlabeled nodes in this scheme

	// Initialize initial backup nodes count if not set
	if initialBackupNodesCount == 0 {
		initialBackupNodesCount = len(backupNodes)
		fmt.Printf("Initial backup nodes count set to: %d\n", initialBackupNodesCount)
	}

	// Initialize initial primary nodes count if not set
	if initPrimaryNodesCount == 0 {
		initPrimaryNodesCount = len(primaryNodes)
		fmt.Printf("Initial primary nodes count set to: %d\n", initPrimaryNodesCount)
	}

	fmt.Printf("Total nodes: %d\n", len(nodes.Items))
	fmt.Printf("Primary nodes: %d\n", len(primaryNodes))
	fmt.Printf("Backup nodes: %d\n", len(backupNodes))
	fmt.Printf("Initial backup nodes count: %d\n", initialBackupNodesCount)
	fmt.Printf("Initial primary nodes count: %d\n", initPrimaryNodesCount)
	fmt.Printf("Unlabeled nodes: %d\n", len(unlabeledNodes))

	// Print zone distribution if multi-AZ is enabled
	if enableMultiAZ {
		zoneMap := classifyNodesByZone(nodes.Items)
		printZoneDistribution(zoneMap)
	}

	// Check primary nodes status
	for _, node := range primaryNodes {
		checkNodeStatus(node, "primary", clientset)
	}

	// Check backup nodes status
	for _, node := range backupNodes {
		checkNodeStatus(node, "backup", clientset)
	}

	// Handle unlabeled nodes
	if len(unlabeledNodes) > 0 {
		fmt.Println("\nUnlabeled nodes found:")
		for _, node := range unlabeledNodes {
			fmt.Printf("  - %s\n", node.Name)
		}
		fmt.Println("Consider labeling these nodes using:")
		fmt.Println("  nodecontroller label <nodes> --role=<primary|backup>")
	}
}

func checkNodeStatus(node corev1.Node, role string, clientset *kubernetes.Clientset) {
	fmt.Printf("\nChecking %s node: %s\n", role, node.Name)

	// Check node condition
	isReady := false
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
			isReady = true
			break
		}
	}

	if !isReady {
		fmt.Printf("  WARNING: Node %s is not ready!\n", node.Name)
		// Automatic failover logic
		if role == "primary" {
			fmt.Printf("  PRIMARY NODE DOWN: Initiating failover procedures...\n")

			// Get node's zone
			nodeZone := getNodeZone(node)
			fmt.Printf("  Node zone: %s\n", nodeZone)

			// 1. Check if there are available backup nodes
			backupNodesCount := checkAvailableBackupNodes(clientset)
			if backupNodesCount > 0 {
				fmt.Printf("  INFO: Found %d available backup nodes for failover\n", backupNodesCount)
			} else {
				fmt.Printf("  CRITICAL: No available backup nodes for failover!\n")
			}

			// 2. Mark the node as failed (add a label)
			fmt.Printf("  INFO: Marking node %s as failed\n", node.Name)
			markNodeAsFailed(node.Name)

			// 3. Check pods on the failed node
			podsOnFailedNode := getPodsOnNode(clientset, node.Name)
			fmt.Printf("  INFO: Found %d pods on failed node %s\n", len(podsOnFailedNode), node.Name)

			// 4. For each pod, check if it will be rescheduled
			for _, pod := range podsOnFailedNode {
				if pod.Spec.RestartPolicy == corev1.RestartPolicyAlways ||
					pod.Spec.RestartPolicy == corev1.RestartPolicyOnFailure {
					fmt.Printf("  INFO: Pod %s in namespace %s will be automatically rescheduled\n",
						pod.Name, pod.Namespace)
				} else {
					fmt.Printf("  WARNING: Pod %s in namespace %s has RestartPolicy=Never and may not be rescheduled\n",
						pod.Name, pod.Namespace)
				}
			}

			// 5. Check if there are other available primary nodes
			availablePrimaryNodes := countAvailablePrimaryNodes(clientset)
			if availablePrimaryNodes == 0 {
				fmt.Printf("  CRITICAL: No primary nodes available! Promoting backup nodes...\n")
				promoteBackupNodes(clientset)
			} else {
				// Check if there are available primary nodes in the same zone
				availablePrimaryNodesInZone := 0
				if enableMultiAZ && nodeZone != "" {
					availablePrimaryNodesInZone = countAvailablePrimaryNodesInZone(clientset, nodeZone)
					fmt.Printf("  INFO: Found %d available primary nodes in zone %s\n", availablePrimaryNodesInZone, nodeZone)
				}

				if availablePrimaryNodesInZone == 0 {
					fmt.Printf("  CRITICAL: No primary nodes available in zone %s! Promoting backup nodes...\n", nodeZone)
					promoteBackupNodesInZone(clientset, nodeZone)
				} else {
					fmt.Printf("  INFO: Found %d available primary nodes, no need to promote backup nodes\n", availablePrimaryNodes)
				}
			}

			// 6. Optional: Send notification (could be extended to use webhooks, etc.)
			fmt.Printf("  INFO: Failover procedures completed for node %s\n", node.Name)
		} else if role == "backup" {
			fmt.Printf("  WARNING: Backup node %s is not ready!\n", node.Name)
			fmt.Printf("  This reduces failover capacity\n")
		}
	} else {
		// Node is ready, check if it was previously failed
		if _, ok := node.Labels["node-role.kubernetes.io/failed"]; ok {
			fmt.Printf("  INFO: Node %s has recovered from failure!\n", node.Name)
			handleNodeRecovery(node, clientset)
		}
	}

	// Check resource usage
	cmd := exec.Command("kubectl", "top", "node", node.Name)
	output, err := cmd.CombinedOutput()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		if len(lines) > 1 {
			usageLine := lines[1]
			fields := strings.Fields(usageLine)
			if len(fields) >= 3 {
				cpuUsage := fields[1]
				memUsage := fields[2]
				fmt.Printf("  Resource usage: CPU=%s, Memory=%s\n", cpuUsage, memUsage)

				// Check if usage exceeds threshold
				if role == "primary" {
					if checkResourceThreshold(cpuUsage) || checkResourceThreshold(memUsage) {
						fmt.Printf("  WARNING: Resource usage exceeds threshold!\n")

						// Check if there are other available primary nodes
						availablePrimaryNodes := countAvailablePrimaryNodes(clientset)
						if availablePrimaryNodes <= 1 {
							// Only one primary node available, need to promote backup nodes
							fmt.Printf("  WARNING: Only %d primary node available. Promoting backup nodes...\n", availablePrimaryNodes)
							promoteBackupNodes(clientset)
						} else {
							// Check if there are available primary nodes in the same zone
							nodeZone := getNodeZone(node)
							availablePrimaryNodesInZone := 0
							if enableMultiAZ && nodeZone != "" {
								availablePrimaryNodesInZone = countAvailablePrimaryNodesInZone(clientset, nodeZone)
								fmt.Printf("  INFO: Found %d available primary nodes in zone %s\n", availablePrimaryNodesInZone, nodeZone)
							}

							if availablePrimaryNodesInZone <= 1 {
								// Only one primary node available in this zone, need to promote backup nodes in the same zone
								fmt.Printf("  WARNING: Only %d primary node available in zone %s. Promoting backup nodes...\n", availablePrimaryNodesInZone, nodeZone)
								promoteBackupNodesInZone(clientset, nodeZone)
							} else {
								// Other primary nodes available in this zone, mark this node as overloaded
								fmt.Printf("  INFO: Found %d available primary nodes, marking this node as overloaded\n", availablePrimaryNodes)
								markNodeAsOverloaded(node.Name)
							}
						}
					} else {
						// Resource usage is normal, remove overload taint if present
						removeNodeOverloadTaint(node.Name)
					}
				}
			}
		}
	}

	// Check if node has correct label
	hasCorrectLabel := false
	if role == "primary" {
		if val, ok := node.Labels["node-role.kubernetes.io/role"]; ok {
			hasCorrectLabel = val == "primary"
		}
	} else if role == "backup" {
		// Backup nodes should not have any role label
		_, hasLabel := node.Labels["node-role.kubernetes.io/role"]
		hasCorrectLabel = !hasLabel
	}

	if !hasCorrectLabel {
		fmt.Printf("  WARNING: Node %s has incorrect labels!\n", node.Name)
		fmt.Printf("  Running label command to fix...\n")
		labelNode(node.Name, role)
	}
}

func checkResourceThreshold(usage string) bool {
	// Extract percentage from usage string (e.g., "100m" or "50%")
	if strings.HasSuffix(usage, "%") {
		usageStr := strings.TrimSuffix(usage, "%")
		var usagePercent float64
		fmt.Sscanf(usageStr, "%f", &usagePercent)
		return usagePercent > resourceThreshold
	}
	return false
}

func checkAvailableBackupNodes(clientset *kubernetes.Clientset) int {
	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return 0
	}

	count := 0
	for _, node := range nodes.Items {
		// Check if node is a backup node (no role label)
		if _, ok := node.Labels["node-role.kubernetes.io/role"]; !ok {
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

func countAvailablePrimaryNodes(clientset *kubernetes.Clientset) int {
	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return 0
	}

	count := 0
	for _, node := range nodes.Items {
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

func markNodeAsFailed(nodeName string) {
	// Add a label to mark the node as failed
	cmd := exec.Command("kubectl", "label", "nodes", nodeName, "node-role.kubernetes.io/failed=true", "--overwrite")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("  ERROR: Failed to mark node %s as failed: %s\n", nodeName, string(output))
	}
}

func markNodeAsOverloaded(nodeName string) {
	// Add overload taint to prevent new pods from scheduling
	cmd := exec.Command("kubectl", "taint", "nodes", nodeName, "node-role.kubernetes.io/overloaded:NoSchedule", "--overwrite")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("  ERROR: Failed to mark node %s as overloaded: %s\n", nodeName, string(output))
	} else {
		fmt.Printf("  INFO: Node %s marked as overloaded, new pods will not be scheduled here\n", nodeName)
	}
}

func removeNodeOverloadTaint(nodeName string) {
	// Remove overload taint to allow new pods to schedule
	cmd := exec.Command("kubectl", "taint", "nodes", nodeName, "node-role.kubernetes.io/overloaded:", "-")
	output, err := cmd.CombinedOutput()
	if err == nil && len(output) > 0 {
		fmt.Printf("  INFO: Node %s overload taint removed, new pods can be scheduled here\n", nodeName)
	}
}

func handleNodeRecovery(node corev1.Node, clientset *kubernetes.Clientset) {
	// Remove failed label
	cmd := exec.Command("kubectl", "label", "nodes", node.Name, "node-role.kubernetes.io/failed-", "--overwrite")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("  ERROR: Failed to remove failed label from node %s: %s\n", node.Name, string(output))
	} else {
		fmt.Printf("  INFO: Successfully removed failed label from node %s\n", node.Name)
	}

	// Remove overload taint if present
	removeNodeOverloadTaint(node.Name)

	// Get node's zone
	nodeZone := getNodeZone(node)
	fmt.Printf("  Node zone: %s\n", nodeZone)

	// Check if node is a primary node
	if val, ok := node.Labels["node-role.kubernetes.io/role"]; ok && val == "primary" {
		fmt.Printf("  INFO: Primary node %s has recovered\n", node.Name)

		// Check current backup nodes count
		currentBackupNodesCount := checkAvailableBackupNodes(clientset)
		fmt.Printf("  INFO: Current backup nodes count: %d, Initial: %d\n", currentBackupNodesCount, initialBackupNodesCount)

		// Check backup nodes count in the same zone if multi-AZ is enabled
		if enableMultiAZ && nodeZone != "" {
			// Check if there are promoted backup nodes in the same zone that can be demoted
			demotePromotedBackupNodesInZone(clientset, nodeZone)

			// Check if backup nodes count in the same zone is sufficient
			currentBackupNodesCountInZone := checkAvailableBackupNodesInZone(clientset, nodeZone)
			fmt.Printf("  INFO: Current backup nodes count in zone %s: %d\n", nodeZone, currentBackupNodesCountInZone)

			// If backup nodes count in the same zone is less than initial per zone, demote this node to backup
			initialBackupNodesCountPerZone := initialBackupNodesCount / 3 // Assume 3 zones
			if initialBackupNodesCountPerZone < 1 {
				initialBackupNodesCountPerZone = 1
			}

			if currentBackupNodesCountInZone < initialBackupNodesCountPerZone {
				fmt.Printf("  INFO: Backup nodes count in zone %s is less than initial. Demoting node %s to backup\n", nodeZone, node.Name)
				demoteNodeToBackup(node.Name)
			} else {
				fmt.Printf("  INFO: Backup nodes count in zone %s is sufficient. Keeping node %s as primary\n", nodeZone, node.Name)
			}
		} else {
			// If backup nodes count is less than initial, demote this node to backup
			if currentBackupNodesCount < initialBackupNodesCount {
				fmt.Printf("  INFO: Backup nodes count is less than initial. Demoting node %s to backup\n", node.Name)
				demoteNodeToBackup(node.Name)
			} else {
				fmt.Printf("  INFO: Backup nodes count is sufficient. Keeping node %s as primary\n", node.Name)
				// Check if there are promoted backup nodes that can be demoted
				demotePromotedBackupNodes(clientset)
			}
		}
	} else {
		fmt.Printf("  INFO: Backup node %s has recovered\n", node.Name)
	}

	// Check pods on the recovered node
	podsOnRecoveredNode := getPodsOnNode(clientset, node.Name)
	if len(podsOnRecoveredNode) > 0 {
		fmt.Printf("  INFO: Found %d pods on recovered node %s\n", len(podsOnRecoveredNode), node.Name)
		fmt.Printf("  INFO: Pods will continue running on this node\n")
	}
}

func demoteNodeToBackup(nodeName string) {
	// Remove primary label and promoted label
	cmd := exec.Command("kubectl", "label", "nodes", nodeName, "node-role.kubernetes.io/role-", "node-role.kubernetes.io/promoted-", "--overwrite")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("  ERROR: Failed to demote node %s: %s\n", nodeName, string(output))
	} else {
		fmt.Printf("  SUCCESS: Node %s has been demoted back to backup\n", nodeName)
	}
}

func demotePromotedBackupNodes(clientset *kubernetes.Clientset) {
	// Get all nodes
	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		fmt.Printf("  ERROR: Failed to list nodes: %v\n", err)
		return
	}

	// If multi-AZ is enabled, use zone-aware demotion logic
	if enableMultiAZ {
		zoneMap := classifyNodesByZone(nodes.Items)
		printZoneDistribution(zoneMap)

		selectedNodeName := selectPromotedNodeToDemoteByZone(zoneMap, clientset)
		if selectedNodeName != "" {
			fmt.Printf("  INFO: Demoting promoted node %s back to backup\n", selectedNodeName)
			demoteNodeToBackup(selectedNodeName)
		}
		return
	}

	// Count available primary nodes
	availablePrimaryNodes := countAvailablePrimaryNodes(clientset)

	// If we have more than initial primary nodes count, consider demoting some
	if availablePrimaryNodes > initPrimaryNodesCount {
		fmt.Printf("  INFO: Found %d primary nodes, considering demoting promoted backup nodes\n", availablePrimaryNodes)

		// Find promoted backup nodes (nodes that were originally backup but now have primary label)
		promotedNodes := []string{}
		for _, node := range nodes.Items {
			if val, ok := node.Labels["node-role.kubernetes.io/role"]; ok && val == "primary" {
				// Check if node has a promoted label (we'll add this when promoting)
				if _, promoted := node.Labels["node-role.kubernetes.io/promoted"]; promoted {
					promotedNodes = append(promotedNodes, node.Name)
				}
			}
		}

		// Demote promoted backup nodes until we have initial primary nodes count
		nodesToDemote := len(promotedNodes) - (availablePrimaryNodes - initPrimaryNodesCount)
		for i := 0; i < nodesToDemote && i < len(promotedNodes); i++ {
			nodeToDemote := promotedNodes[i]
			fmt.Printf("  INFO: Demoting promoted node %s back to backup\n", nodeToDemote)
			demoteNodeToBackup(nodeToDemote)
		}
	}
}

func getPodsOnNode(clientset *kubernetes.Clientset, nodeName string) []corev1.Pod {
	pods, err := clientset.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
	})
	if err != nil {
		return []corev1.Pod{}
	}

	return pods.Items
}

func promoteBackupNodes(clientset *kubernetes.Clientset) {
	// Get all nodes
	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		fmt.Printf("  ERROR: Failed to list nodes: %v\n", err)
		return
	}

	// If multi-AZ is enabled, use zone-aware promotion logic
	if enableMultiAZ {
		zoneMap := classifyNodesByZone(nodes.Items)
		printZoneDistribution(zoneMap)

		selectedNodeName := selectBackupNodeByZone(zoneMap, clientset)
		if selectedNodeName == "" {
			fmt.Printf("  ERROR: No available backup nodes to promote\n")
			return
		}

		// Add primary label and promoted label to the backup node
		cmd := exec.Command("kubectl", "label", "nodes", selectedNodeName, "node-role.kubernetes.io/role=primary", "node-role.kubernetes.io/promoted=true", "--overwrite")
		output, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Printf("  ERROR: Failed to promote node %s: %s\n", selectedNodeName, string(output))
		} else {
			fmt.Printf("  SUCCESS: Node %s has been promoted to primary\n", selectedNodeName)
		}
		return
	}

	// Find available backup nodes (nodes without role label) and their available resources
	type backupNodeWithResources struct {
		name            string
		availableCPU    int64
		availableMemory int64
	}

	availableBackupNodes := []backupNodeWithResources{}

	for _, node := range nodes.Items {
		if _, ok := node.Labels["node-role.kubernetes.io/role"]; !ok {
			// Check if node is ready
			isReady := false
			for _, condition := range node.Status.Conditions {
				if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
					isReady = true
					break
				}
			}
			if isReady {
				// Calculate available resources
				var availableCPU int64
				var availableMemory int64

				for resourceName, quantity := range node.Status.Allocatable {
					if resourceName == "cpu" {
						availableCPU = quantity.MilliValue()
					} else if resourceName == "memory" {
						availableMemory = quantity.Value()
					}
				}

				for resourceName, quantity := range node.Status.Capacity {
					if resourceName == "cpu" && availableCPU == 0 {
						availableCPU = quantity.MilliValue()
					} else if resourceName == "memory" && availableMemory == 0 {
						availableMemory = quantity.Value()
					}
				}

				availableBackupNodes = append(availableBackupNodes, backupNodeWithResources{
					name:            node.Name,
					availableCPU:    availableCPU,
					availableMemory: availableMemory,
				})
			}
		}
	}

	if len(availableBackupNodes) == 0 {
		fmt.Printf("  ERROR: No available backup nodes to promote\n")
		return
	}

	// Select the backup node with the most available resources (using CPU as primary metric, memory as secondary)
	selectedNode := availableBackupNodes[0]
	for _, node := range availableBackupNodes {
		if node.availableCPU > selectedNode.availableCPU {
			selectedNode = node
		} else if node.availableCPU == selectedNode.availableCPU && node.availableMemory > selectedNode.availableMemory {
			selectedNode = node
		}
	}

	fmt.Printf("  INFO: Promoting backup node %s to primary (Available CPU: %dm, Memory: %dMi)\n",
		selectedNode.name, selectedNode.availableCPU, selectedNode.availableMemory/(1024*1024))

	// Add primary label and promoted label to the backup node
	cmd := exec.Command("kubectl", "label", "nodes", selectedNode.name, "node-role.kubernetes.io/role=primary", "node-role.kubernetes.io/promoted=true", "--overwrite")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("  ERROR: Failed to promote node %s: %s\n", selectedNode.name, string(output))
	} else {
		fmt.Printf("  SUCCESS: Node %s has been promoted to primary\n", selectedNode.name)
	}
}

type zoneInfo struct {
	zoneLabel    string
	primaryNodes []corev1.Node
	backupNodes  []corev1.Node
	primaryCount int
	backupCount  int
}

func getNodeZone(node corev1.Node) string {
	if zone, ok := node.Labels["topology.kubernetes.io/zone"]; ok {
		return zone
	}
	return ""
}

func classifyNodesByZone(nodes []corev1.Node) map[string]*zoneInfo {
	zoneMap := make(map[string]*zoneInfo)

	for _, node := range nodes {
		zone := getNodeZone(node)
		if zone == "" {
			continue
		}

		if _, exists := zoneMap[zone]; !exists {
			zoneMap[zone] = &zoneInfo{
				zoneLabel:    zone,
				primaryNodes: []corev1.Node{},
				backupNodes:  []corev1.Node{},
			}
		}

		if val, ok := node.Labels["node-role.kubernetes.io/role"]; ok && val == "primary" {
			zoneMap[zone].primaryNodes = append(zoneMap[zone].primaryNodes, node)
		} else {
			zoneMap[zone].backupNodes = append(zoneMap[zone].backupNodes, node)
		}
	}

	for _, info := range zoneMap {
		info.primaryCount = len(info.primaryNodes)
		info.backupCount = len(info.backupNodes)
	}

	return zoneMap
}

func printZoneDistribution(zoneMap map[string]*zoneInfo) {
	if !enableMultiAZ {
		return
	}

	fmt.Println("\n=== Multi-AZ Node Distribution ===")
	for zone, info := range zoneMap {
		fmt.Printf("Zone %s:\n", zone)
		fmt.Printf("  Primary nodes: %d\n", info.primaryCount)
		for _, node := range info.primaryNodes {
			fmt.Printf("    - %s\n", node.Name)
		}
		fmt.Printf("  Backup nodes: %d\n", info.backupCount)
		for _, node := range info.backupNodes {
			fmt.Printf("    - %s\n", node.Name)
		}
	}
}

func selectBackupNodeByZone(zoneMap map[string]*zoneInfo, clientset *kubernetes.Clientset) string {
	if !enableMultiAZ {
		return ""
	}

	type backupNodeWithResources struct {
		name            string
		zone            string
		availableCPU    int64
		availableMemory int64
	}

	availableBackupNodes := []backupNodeWithResources{}

	for zone, info := range zoneMap {
		for _, node := range info.backupNodes {
			isReady := false
			for _, condition := range node.Status.Conditions {
				if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
					isReady = true
					break
				}
			}

			if isReady {
				var availableCPU int64
				var availableMemory int64

				for resourceName, quantity := range node.Status.Allocatable {
					if resourceName == "cpu" {
						availableCPU = quantity.MilliValue()
					} else if resourceName == "memory" {
						availableMemory = quantity.Value()
					}
				}

				for resourceName, quantity := range node.Status.Capacity {
					if resourceName == "cpu" && availableCPU == 0 {
						availableCPU = quantity.MilliValue()
					} else if resourceName == "memory" && availableMemory == 0 {
						availableMemory = quantity.Value()
					}
				}

				availableBackupNodes = append(availableBackupNodes, backupNodeWithResources{
					name:            node.Name,
					zone:            zone,
					availableCPU:    availableCPU,
					availableMemory: availableMemory,
				})
			}
		}
	}

	if len(availableBackupNodes) == 0 {
		return ""
	}

	zonePrimaryCounts := make(map[string]int)
	for zone, info := range zoneMap {
		zonePrimaryCounts[zone] = info.primaryCount
	}

	minPrimaryCount := int(^uint(0) >> 1)
	for _, count := range zonePrimaryCounts {
		if count < minPrimaryCount {
			minPrimaryCount = count
		}
	}

	minZoneNodes := []backupNodeWithResources{}
	for _, node := range availableBackupNodes {
		if zonePrimaryCounts[node.zone] == minPrimaryCount {
			minZoneNodes = append(minZoneNodes, node)
		}
	}

	if len(minZoneNodes) == 0 {
		minZoneNodes = availableBackupNodes
	}

	selectedNode := minZoneNodes[0]
	for _, node := range minZoneNodes {
		if node.availableCPU > selectedNode.availableCPU {
			selectedNode = node
		} else if node.availableCPU == selectedNode.availableCPU && node.availableMemory > selectedNode.availableMemory {
			selectedNode = node
		}
	}

	fmt.Printf("  INFO: Selected backup node %s from zone %s for promotion (Available CPU: %dm, Memory: %dMi)\n",
		selectedNode.name, selectedNode.zone, selectedNode.availableCPU, selectedNode.availableMemory/(1024*1024))

	return selectedNode.name
}

func selectPromotedNodeToDemoteByZone(zoneMap map[string]*zoneInfo, clientset *kubernetes.Clientset) string {
	if !enableMultiAZ {
		return ""
	}

	availablePrimaryNodes := countAvailablePrimaryNodes(clientset)
	if availablePrimaryNodes <= initPrimaryNodesCount {
		return ""
	}

	type promotedNodeWithZone struct {
		name string
		zone string
	}

	promotedNodes := []promotedNodeWithZone{}

	for zone, info := range zoneMap {
		for _, node := range info.primaryNodes {
			if _, promoted := node.Labels["node-role.kubernetes.io/promoted"]; promoted {
				promotedNodes = append(promotedNodes, promotedNodeWithZone{
					name: node.Name,
					zone: zone,
				})
			}
		}
	}

	if len(promotedNodes) == 0 {
		return ""
	}

	zonePrimaryCounts := make(map[string]int)
	for zone, info := range zoneMap {
		zonePrimaryCounts[zone] = info.primaryCount
	}

	maxPrimaryCount := 0
	for _, count := range zonePrimaryCounts {
		if count > maxPrimaryCount {
			maxPrimaryCount = count
		}
	}

	maxZoneNodes := []promotedNodeWithZone{}
	for _, node := range promotedNodes {
		if zonePrimaryCounts[node.zone] == maxPrimaryCount {
			maxZoneNodes = append(maxZoneNodes, node)
		}
	}

	if len(maxZoneNodes) > 0 {
		return maxZoneNodes[0].name
	}

	return promotedNodes[0].name
}

func countAvailablePrimaryNodesInZone(clientset *kubernetes.Clientset, zone string) int {
	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
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

func promoteBackupNodesInZone(clientset *kubernetes.Clientset, zone string) {
	// Get all nodes
	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		fmt.Printf("  ERROR: Failed to list nodes: %v\n", err)
		return
	}

	// Find available backup nodes in the specified zone (nodes without role label)
	type backupNodeWithResources struct {
		name            string
		availableCPU    int64
		availableMemory int64
	}

	availableBackupNodes := []backupNodeWithResources{}

	for _, node := range nodes.Items {
		// Check if node is in the specified zone
		if nodeZone := getNodeZone(node); nodeZone != zone {
			continue
		}

		// Check if node is a backup node (no role label)
		if _, ok := node.Labels["node-role.kubernetes.io/role"]; !ok {
			// Check if node is ready
			isReady := false
			for _, condition := range node.Status.Conditions {
				if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
					isReady = true
					break
				}
			}
			if isReady {
				// Calculate available resources
				var availableCPU int64
				var availableMemory int64

				for resourceName, quantity := range node.Status.Allocatable {
					if resourceName == "cpu" {
						availableCPU = quantity.MilliValue()
					} else if resourceName == "memory" {
						availableMemory = quantity.Value()
					}
				}

				for resourceName, quantity := range node.Status.Capacity {
					if resourceName == "cpu" && availableCPU == 0 {
						availableCPU = quantity.MilliValue()
					} else if resourceName == "memory" && availableMemory == 0 {
						availableMemory = quantity.Value()
					}
				}

				availableBackupNodes = append(availableBackupNodes, backupNodeWithResources{
					name:            node.Name,
					availableCPU:    availableCPU,
					availableMemory: availableMemory,
				})
			}
		}
	}

	if len(availableBackupNodes) == 0 {
		fmt.Printf("  ERROR: No available backup nodes in zone %s to promote\n", zone)
		return
	}

	// Select the backup node with the most available resources (using CPU as primary metric, memory as secondary)
	selectedNode := availableBackupNodes[0]
	for _, node := range availableBackupNodes {
		if node.availableCPU > selectedNode.availableCPU {
			selectedNode = node
		} else if node.availableCPU == selectedNode.availableCPU && node.availableMemory > selectedNode.availableMemory {
			selectedNode = node
		}
	}

	fmt.Printf("  INFO: Promoting backup node %s from zone %s to primary (Available CPU: %dm, Memory: %dMi)\n",
		selectedNode.name, zone, selectedNode.availableCPU, selectedNode.availableMemory/(1024*1024))

	// Add primary label and promoted label to the backup node
	cmd := exec.Command("kubectl", "label", "nodes", selectedNode.name, "node-role.kubernetes.io/role=primary", "node-role.kubernetes.io/promoted=true", "--overwrite")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("  ERROR: Failed to promote node %s: %s\n", selectedNode.name, string(output))
	} else {
		fmt.Printf("  SUCCESS: Node %s has been promoted to primary\n", selectedNode.name)
	}
}

func checkAvailableBackupNodesInZone(clientset *kubernetes.Clientset, zone string) int {
	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return 0
	}

	count := 0
	for _, node := range nodes.Items {
		// Check if node is in the specified zone
		if nodeZone := getNodeZone(node); nodeZone != zone {
			continue
		}

		// Check if node is a backup node (no role label)
		if _, ok := node.Labels["node-role.kubernetes.io/role"]; !ok {
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

func demotePromotedBackupNodesInZone(clientset *kubernetes.Clientset, zone string) {
	// Get all nodes
	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		fmt.Printf("  ERROR: Failed to list nodes: %v\n", err)
		return
	}

	// Find promoted backup nodes in the specified zone (nodes that were originally backup but now have primary label)
	promotedNodes := []string{}
	for _, node := range nodes.Items {
		// Check if node is in the specified zone
		if nodeZone := getNodeZone(node); nodeZone != zone {
			continue
		}

		if val, ok := node.Labels["node-role.kubernetes.io/role"]; ok && val == "primary" {
			// Check if node has a promoted label
			if _, promoted := node.Labels["node-role.kubernetes.io/promoted"]; promoted {
				promotedNodes = append(promotedNodes, node.Name)
			}
		}
	}

	if len(promotedNodes) == 0 {
		return
	}

	// Demote the first promoted backup node
	nodeToDemote := promotedNodes[0]
	fmt.Printf("  INFO: Demoting promoted node %s back to backup\n", nodeToDemote)
	demoteNodeToBackup(nodeToDemote)
}
