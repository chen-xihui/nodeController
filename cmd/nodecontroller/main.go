package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	"nodecontroller/pkg/node"
	"nodecontroller/pkg/zone"
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
}

func handleAutoCommand() {
	// Set autoMode to true
	autoMode = true

	// Build configuration
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		log.Fatalf("Error building kubeconfig: %v", err)
	}

	// Create clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error creating clientset: %v", err)
	}

	// Initialize node manager
	nodeManager := node.NewNodeManager(clientset, resourceThreshold, enableMultiAZ)

	// Run auto mode
	fmt.Println("Starting Node Controller in automatic mode...")
	fmt.Printf("Check interval: %v\n", interval)
	fmt.Printf("Resource usage threshold: %.2f%%\n", resourceThreshold)
	fmt.Printf("Multi-AZ support: %v\n", enableMultiAZ)

	// Main loop
	for {
		fmt.Println("\n=== Checking node status ===")
		nodeManager.CheckNodeStatus()

		// Sleep until next check
		time.Sleep(interval)
	}
}

func handleLabelCommand() {
	// Handle label command
	if len(os.Args) < 3 {
		fmt.Println("Error: Please specify nodes to label")
		printUsage()
		return
	}

	// Get role flag
	role := ""
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "--role=") {
			role = strings.TrimPrefix(arg, "--role=")
			break
		}
	}

	if role == "" || (role != "primary" && role != "backup") {
		fmt.Println("Error: Please specify valid role --role=primary or --role=backup")
		printUsage()
		return
	}

	// Get nodes to label
	nodes := []string{}
	for i := 2; i < len(os.Args); i++ {
		if !strings.HasPrefix(os.Args[i], "--") {
			nodes = append(nodes, os.Args[i])
		}
	}

	if len(nodes) == 0 {
		fmt.Println("Error: Please specify nodes to label")
		printUsage()
		return
	}

	// Label nodes
	for _, nodeName := range nodes {
		var cmd *exec.Cmd
		if role == "primary" {
			cmd = exec.Command("kubectl", "label", "nodes", nodeName, "node-role.kubernetes.io/role=primary", "--overwrite")
		} else {
			cmd = exec.Command("kubectl", "label", "nodes", nodeName, "node-role.kubernetes.io/role-")
		}

		output, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Printf("Error labeling node %s: %s\n", nodeName, string(output))
		} else {
			fmt.Printf("Successfully labeled node %s as %s\n", nodeName, role)
		}
	}
}

func handleTaintCommand() {
	// Handle taint command
	if len(os.Args) < 3 {
		fmt.Println("Error: Please specify nodes to taint")
		printUsage()
		return
	}

	// Get role flag
	role := ""
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "--role=") {
			role = strings.TrimPrefix(arg, "--role=")
			break
		}
	}

	if role == "" || (role != "primary" && role != "backup") {
		fmt.Println("Error: Please specify valid role --role=primary or --role=backup")
		printUsage()
		return
	}

	// Get nodes to taint
	nodes := []string{}
	for i := 2; i < len(os.Args); i++ {
		if !strings.HasPrefix(os.Args[i], "--") {
			nodes = append(nodes, os.Args[i])
		}
	}

	if len(nodes) == 0 {
		fmt.Println("Error: Please specify nodes to taint")
		printUsage()
		return
	}

	// Taint nodes
	for _, nodeName := range nodes {
		var cmd *exec.Cmd
		if role == "primary" {
			cmd = exec.Command("kubectl", "taint", "nodes", nodeName, "node-role.kubernetes.io/backup:")
		} else {
			cmd = exec.Command("kubectl", "taint", "nodes", nodeName, "node-role.kubernetes.io/backup:NoSchedule", "--overwrite")
		}

		output, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Printf("Error tainting node %s: %s\n", nodeName, string(output))
		} else {
			fmt.Printf("Successfully tainted node %s for role %s\n", nodeName, role)
		}
	}
}

func handleStatusCommand() {
	// Handle status command
	var cmd *exec.Cmd

	if len(os.Args) > 2 {
		// Get specific nodes
		nodes := os.Args[2:]
		cmd = exec.Command("kubectl", append([]string{"get", "nodes"}, nodes...)...)
	} else {
		// Get all nodes
		cmd = exec.Command("kubectl", "get", "nodes", "--show-labels")
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("Error getting node status: %s\n", string(output))
		return
	}

	fmt.Println(string(output))
}

func handleMonitorCommand() {
	// Handle monitor command
	fmt.Println("Starting Node Controller in monitor mode...")

	// Build configuration
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		log.Fatalf("Error building kubeconfig: %v", err)
	}

	// Create clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error creating clientset: %v", err)
	}

	// Initialize zone manager
	zoneManager := zone.NewZoneManager(clientset)

	// Run monitor mode
	for {
		fmt.Println("\n=== Monitoring node status ===")

		// Get nodes
		nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			fmt.Printf("Error listing nodes: %v\n", err)
			time.Sleep(interval)
			continue
		}

		// Print zone distribution
		if enableMultiAZ {
			zoneMap := zoneManager.ClassifyNodesByZone(nodes.Items)
			zoneManager.PrintZoneDistribution(zoneMap)
		}

		// Print node status
		for _, nodeItem := range nodes.Items {
			fmt.Printf("Node: %s\n", nodeItem.Name)
			fmt.Printf("  Status: %s\n", getNodeStatus(nodeItem))
			fmt.Printf("  Role: %s\n", getNodeRole(nodeItem))
			fmt.Printf("  Zone: %s\n", zoneManager.GetNodeZone(nodeItem))
		}

		// Sleep until next check
		time.Sleep(interval)
	}
}

func getNodeStatus(nodeItem corev1.Node) string {
	for _, condition := range nodeItem.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			if condition.Status == corev1.ConditionTrue {
				return "Ready"
			} else {
				return "Not Ready"
			}
		}
	}
	return "Unknown"
}

func getNodeRole(nodeItem corev1.Node) string {
	if val, ok := nodeItem.Labels["node-role.kubernetes.io/role"]; ok && val == "primary" {
		return "Primary"
	}
	return "Backup"
}
