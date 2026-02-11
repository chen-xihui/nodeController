package zone

import (
	"fmt"
	"math/rand"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// ZoneInfo contains information about nodes in a zone
type ZoneInfo struct {
	ZoneLabel    string
	PrimaryNodes []corev1.Node
	BackupNodes  []corev1.Node
	PrimaryCount int
	BackupCount  int
}

// ZoneManager handles zone-related operations
type ZoneManager struct {
	Clientset *kubernetes.Clientset
}

// NewZoneManager creates a new ZoneManager
func NewZoneManager(clientset *kubernetes.Clientset) *ZoneManager {
	return &ZoneManager{
		Clientset: clientset,
	}
}

// ClassifyNodesByZone classifies nodes by their zones
func (zm *ZoneManager) ClassifyNodesByZone(nodes []corev1.Node) map[string]*ZoneInfo {
	zoneMap := make(map[string]*ZoneInfo)

	for _, node := range nodes {
		zone := zm.GetNodeZone(node)
		if zone == "" {
			continue
		}

		if _, exists := zoneMap[zone]; !exists {
			zoneMap[zone] = &ZoneInfo{
				ZoneLabel:    zone,
				PrimaryNodes: []corev1.Node{},
				BackupNodes:  []corev1.Node{},
			}
		}

		if val, ok := node.Labels["node-role.kubernetes.io/role"]; ok && val == "primary" {
			zoneMap[zone].PrimaryNodes = append(zoneMap[zone].PrimaryNodes, node)
		} else {
			zoneMap[zone].BackupNodes = append(zoneMap[zone].BackupNodes, node)
		}
	}

	for _, info := range zoneMap {
		info.PrimaryCount = len(info.PrimaryNodes)
		info.BackupCount = len(info.BackupNodes)
	}

	return zoneMap
}

// GetNodeZone gets the zone of a node
func (zm *ZoneManager) GetNodeZone(node corev1.Node) string {
	if zone, ok := node.Labels["topology.kubernetes.io/zone"]; ok {
		return zone
	}
	return ""
}

// PrintZoneDistribution prints the distribution of nodes across zones
func (zm *ZoneManager) PrintZoneDistribution(zoneMap map[string]*ZoneInfo) {
	fmt.Println("\n=== Multi-AZ Node Distribution ===")
	for zone, info := range zoneMap {
		fmt.Printf("Zone %s:\n", zone)
		fmt.Printf("  Primary nodes: %d\n", info.PrimaryCount)
		for _, node := range info.PrimaryNodes {
			fmt.Printf("    - %s\n", node.Name)
		}
		fmt.Printf("  Backup nodes: %d\n", info.BackupCount)
		for _, node := range info.BackupNodes {
			fmt.Printf("    - %s\n", node.Name)
		}
	}
}

// SelectBackupNodeByZone selects a backup node from the appropriate zone
func (zm *ZoneManager) SelectBackupNodeByZone(zoneMap map[string]*ZoneInfo) string {
	// Find available backup nodes
	type backupNodeWithResources struct {
		name            string
		zone            string
		availableCPU    int64
		availableMemory int64
	}

	availableBackupNodes := []backupNodeWithResources{}

	for zone, info := range zoneMap {
		for _, node := range info.BackupNodes {
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

	// Find zone with minimum primary nodes
	zonePrimaryCounts := make(map[string]int)
	for zone, info := range zoneMap {
		zonePrimaryCounts[zone] = info.PrimaryCount
	}

	minPrimaryCount := int(^uint(0) >> 1)
	for _, count := range zonePrimaryCounts {
		if count < minPrimaryCount {
			minPrimaryCount = count
		}
	}

	// Get nodes from zones with minimum primary count
	minZoneNodes := []backupNodeWithResources{}
	for _, node := range availableBackupNodes {
		if zonePrimaryCounts[node.zone] == minPrimaryCount {
			minZoneNodes = append(minZoneNodes, node)
		}
	}

	if len(minZoneNodes) == 0 {
		minZoneNodes = availableBackupNodes
	}

	// Select node with most available resources
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

// SelectBackupNodeFromOtherZone selects a backup node from another zone when current zone has none
func (zm *ZoneManager) SelectBackupNodeFromOtherZone(zone string, nodes []corev1.Node) string {
	// Find zones with available backup nodes
	type backupNodeWithZone struct {
		name            string
		zone            string
		availableCPU    int64
		availableMemory int64
	}

	otherZoneBackupNodes := []backupNodeWithZone{}

	for _, node := range nodes {
		// Check if node is in a different zone
		nodeZone := zm.GetNodeZone(node)
		if nodeZone == zone {
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

				otherZoneBackupNodes = append(otherZoneBackupNodes, backupNodeWithZone{
					name:            node.Name,
					zone:            nodeZone,
					availableCPU:    availableCPU,
					availableMemory: availableMemory,
				})
			}
		}
	}

	if len(otherZoneBackupNodes) == 0 {
		return ""
	}

	// Count backup nodes per zone
	zoneBackupCounts := make(map[string]int)
	for _, node := range otherZoneBackupNodes {
		zoneBackupCounts[node.zone]++
	}

	// Find zone with most backup nodes
	maxBackupCount := 0
	var maxBackupZone string
	for z, count := range zoneBackupCounts {
		if count > maxBackupCount {
			maxBackupCount = count
			maxBackupZone = z
		}
	}

	// Get backup nodes from the zone with most backup nodes
	maxZoneBackupNodes := []backupNodeWithZone{}
	for _, node := range otherZoneBackupNodes {
		if node.zone == maxBackupZone {
			maxZoneBackupNodes = append(maxZoneBackupNodes, node)
		}
	}

	// Randomly select a node from the max zone
	selectedNodeIndex := rand.Intn(len(maxZoneBackupNodes))
	selectedNode := maxZoneBackupNodes[selectedNodeIndex]

	fmt.Printf("  INFO: No backup nodes in zone %s, selecting node %s from zone %s (which has %d backup nodes)\n",
		zone, selectedNode.name, selectedNode.zone, maxBackupCount)

	return selectedNode.name
}
