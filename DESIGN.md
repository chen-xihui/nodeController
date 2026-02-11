# 主备节点规划方案设计文档

## 1. 项目背景与目标

### 1.1 背景
在大规模Kubernetes集群中，节点的合理规划和管理对集群的稳定性和资源利用率至关重要。传统的节点管理方式难以实现自动化的主备节点切换和资源优化，需要一种更智能的方案来确保服务的高可用性和资源的合理分配。

### 1.2 目标
- 设计一种基于Kubernetes标签和污点的主备节点规划方案
- 实现自动检测节点状态、处理故障转移和资源管理
- 确保主用节点故障时能够快速切换到备用节点
- 当主用节点资源使用率超过阈值时，防止新Pod调度到该节点
- 处理故障节点恢复后的角色调整问题
- 保持备用节点数量稳定，避免因升级导致备用节点减少

## 2. 设计方案概述

### 2.1 核心思路
本方案通过动态管理节点标签来实现主备节点的角色切换，替代传统的污点和容忍度机制，从而简化配置并提高灵活性。

### 2.2 节点角色定义
- **主用节点**：带有 `node-role.kubernetes.io/role=primary` 标签的节点
- **备用节点**：不带 `node-role.kubernetes.io/role` 标签的节点

### 2.3 Pod调度策略
每个Pod在创建时应配置如下nodeAffinity规则：
```yaml
affinity:
  nodeAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      nodeSelectorTerms:
      - matchExpressions:
        - key: node-role.kubernetes.io/role
          operator: In
          values:
          - primary
```

这样，Pod只会被调度到带有 `node-role.kubernetes.io/role=primary` 标签的节点上。

## 3. 标签和污点的作用及设置

### 3.1 标签设置

| 标签名称 | 标签值 | 作用 | 适用节点 |
|---------|-------|------|----------|
| `node-role.kubernetes.io/role` | `primary` | 标识主用节点，Pod会优先调度到此类节点 | 主用节点、升级后的备用节点 |
| `node-role.kubernetes.io/failed` | `true` | 标识故障节点 | 故障的主用节点 |
| `node-role.kubernetes.io/promoted` | `true` | 标识从备用节点升级而来的主用节点 | 升级后的备用节点 |
| `topology.kubernetes.io/zone` | `region-name.az01arm`, `region-name.az02arm`, `region-name.az03arm` | 标识节点所属的可用区 | 所有节点 |

### 3.2 污点设置

| 污点名称 | 污点值 | 效果 | 适用节点 |
|---------|-------|------|----------|
| `node-role.kubernetes.io/overloaded` | `NoSchedule` | 防止新Pod调度到资源过载的节点 | 资源使用率超过90%的主用节点 |

### 3.3 标签和污点的管理策略

1. **初始状态**：
   - 主用节点：添加 `node-role.kubernetes.io/role=primary` 标签
   - 备用节点：无特殊标签
   - 所有节点：添加对应的 `topology.kubernetes.io/zone` 标签

2. **故障转移**：
   - 当主用节点故障时，添加 `node-role.kubernetes.io/failed=true` 标签
   - 从备用节点中选择资源最充足且主用节点数量最少的可用区中的节点，添加 `node-role.kubernetes.io/role=primary` 和 `node-role.kubernetes.io/promoted=true` 标签

3. **资源过载**：
   - 当主用节点资源使用率超过90%时，添加 `node-role.kubernetes.io/overloaded:NoSchedule` 污点
   - 当资源使用率恢复正常时，移除该污点

4. **节点恢复**：
   - 故障节点恢复后，移除 `node-role.kubernetes.io/failed=true` 标签
   - 根据当前备用节点数量和可用区均衡情况，决定是否将恢复的节点降级为备用节点

## 4. 主要处理逻辑

### 4.1 节点状态检测

#### 4.1.1 定期检测
- 每30秒检查一次所有节点的状态
- 检测节点是否就绪（NodeReady状态）
- 检测节点的资源使用率
- 检测节点所属的可用区

#### 4.1.2 故障检测
- 当节点状态变为NotReady时，标记为故障节点
- 当节点资源使用率超过90%时，标记为过载节点

### 4.2 故障转移处理

#### 4.2.1 主用节点故障
1. 检测到主用节点故障
2. 检查当前可用主用节点数量
3. 检查故障节点所属的可用区，统计该可用区的可用主用节点数量
4. 如果该可用区的可用主用节点数量不足，从同一可用区的备用节点中选择节点进行升级：
   - 在同一可用区内，选择资源最充足的节点（CPU优先，内存次之）
5. 为选中的备用节点添加 `node-role.kubernetes.io/role=primary` 和 `node-role.kubernetes.io/promoted=true` 标签

#### 4.2.2 资源过载处理
1. 检测到主用节点资源使用率超过90%
2. 为该节点添加 `node-role.kubernetes.io/overloaded:NoSchedule` 污点
3. 检查过载节点所属的可用区，统计该可用区的可用主用节点数量
4. 如果该可用区的可用主用节点数量不足，从同一可用区的备用节点中选择资源最充足的节点进行升级

### 4.3 节点恢复处理

#### 4.3.1 故障节点恢复
1. 检测到故障节点恢复为Ready状态
2. 移除 `node-role.kubernetes.io/failed=true` 标签
3. 检查恢复节点所属的可用区，统计该可用区的备用节点数量
4. 如果该可用区的备用节点数量少于初始数量，将恢复的节点降级为备用节点
5. 如果该可用区的备用节点数量充足，检查该可用区是否有升级的备用节点可以降级，优先降级该可用区中的升级节点

#### 4.3.2 过载节点恢复
1. 检测到过载节点资源使用率恢复正常
2. 移除 `node-role.kubernetes.io/overloaded:NoSchedule` 污点

### 4.4 备用节点管理

#### 4.4.1 数量控制
- 系统启动时记录初始备用节点数量
- 当备用节点因升级为主用节点而减少时，通过降级操作保持备用节点数量稳定

#### 4.4.2 智能选择
- 当需要升级备用节点时，只考虑同一可用区的备用节点，选择资源最充足的节点
- 当需要降级节点时，只考虑同一可用区的升级节点
- 这样可以确保每个可用区的节点数量保持平衡，避免跨可用区的节点迁移

### 4.5 多可用区支持

#### 4.5.1 可用区划分
- 将集群节点均分为3个可用区：
  - `region-name.az01arm`
  - `region-name.az02arm`
  - `region-name.az03arm`

#### 4.5.2 可用区均衡策略
- **升级策略**：只考虑同一可用区的备用节点进行升级，不跨可用区操作
- **降级策略**：只考虑同一可用区的升级节点进行降级，不跨可用区操作
- **资源平衡**：在同一可用区内，选择资源最充足的节点进行升级
- **数量平衡**：每个可用区的主用节点和备用节点数量保持独立平衡

#### 4.5.3 高可用性保障
- 确保每个可用区都有足够的主用节点和备用节点
- 当某个可用区的主用节点故障时，只从同一可用区的备用节点中选择节点进行升级
- 通过可用区内的节点平衡，提高每个可用区的容错能力
- 避免跨可用区的节点迁移，减少网络延迟和复杂性

## 5. 实现细节

### 5.1 核心组件
- **节点监控器**：定期检测节点状态和资源使用率
- **故障检测器**：识别故障节点并触发故障转移
- **资源管理器**：监控节点资源使用情况，处理过载节点
- **节点调度器**：智能选择备用节点进行升级
- **恢复处理器**：处理故障节点恢复后的角色调整

### 5.2 关键函数

| 函数名 | 功能描述 | 参数 | 返回值 |
|-------|---------|------|-------|
| `checkNodeStatus` | 检查节点状态并处理故障转移 | clientset *kubernetes.Clientset | 无 |
| `promoteBackupNodes` | 升级备用节点为主用节点（多可用区模式下考虑可用区均衡） | clientset *kubernetes.Clientset | 无 |
| `promoteBackupNodesInZone` | 升级指定可用区的备用节点为主用节点 | clientset *kubernetes.Clientset, zone string | 无 |
| `demoteNodeToBackup` | 将节点降级为备用节点 | nodeName string | 无 |
| `demotePromotedBackupNodes` | 降级升级的备用节点（多可用区模式下考虑可用区均衡） | clientset *kubernetes.Clientset | 无 |
| `demotePromotedBackupNodesInZone` | 降级指定可用区的升级节点 | clientset *kubernetes.Clientset, zone string | 无 |
| `handleNodeRecovery` | 处理节点恢复 | node corev1.Node, clientset *kubernetes.Clientset | 无 |
| `markNodeAsOverloaded` | 标记节点为过载 | nodeName string | 无 |
| `removeNodeOverloadTaint` | 移除节点过载污点 | nodeName string | 无 |
| `getNodeZone` | 获取节点所属的可用区 | node corev1.Node | string |
| `countAvailablePrimaryNodesInZone` | 统计指定可用区的可用主用节点数量 | clientset *kubernetes.Clientset, zone string | int |
| `checkAvailableBackupNodesInZone` | 检查指定可用区的可用备用节点数量 | clientset *kubernetes.Clientset, zone string | int |
| `classifyNodesByZone` | 将节点按可用区分类 | nodes []corev1.Node | map[string]*zoneInfo |
| `selectBackupNodeByZone` | 从备用节点中选择节点进行升级，考虑可用区均衡 | zoneMap map[string]*zoneInfo, clientset *kubernetes.Clientset | string |
| `selectPromotedNodeToDemoteByZone` | 选择要降级的节点，考虑可用区均衡 | zoneMap map[string]*zoneInfo, clientset *kubernetes.Clientset | string |

### 5.3 资源管理策略

#### 5.3.1 资源使用率计算
- 计算节点的CPU和内存使用率
- 当使用率超过90%时，标记为过载节点

#### 5.3.2 过载处理
- 为过载节点添加 `node-role.kubernetes.io/overloaded:NoSchedule` 污点
- 这样可以防止新Pod调度到该节点，同时不影响已有的Pod

### 5.4 故障转移策略

#### 5.4.1 触发条件
- 主用节点状态变为NotReady
- 主用节点资源使用率超过90%且无其他可用主用节点

#### 5.4.2 执行流程
1. 检测到主用节点故障或过载
2. 检查当前可用主用节点数量
3. 如果可用主用节点数量不足，从备用节点中选择资源最充足的节点
4. 为选中的备用节点添加主用节点标签
5. 记录升级操作以便后续恢复时处理

## 6. 部署与使用

### 6.1 部署方式
- 将本工具部署为Kubernetes集群中的一个Deployment
- 确保工具具有足够的权限来管理节点标签和污点

### 6.2 配置参数

| 参数名 | 类型 | 默认值 | 描述 |
|-------|------|-------|------|
| `interval` | int | 30 | 节点状态检查间隔（秒） |
| `resourceThreshold` | int | 90 | 资源使用率阈值（%） |
| `multiaz` | bool | false | 启用多可用区支持 |
| `initPrimaryNodesCount` | int | 自动检测 | 初始主用节点数量 |
| `initialBackupNodesCount` | int | 自动检测 | 初始备用节点数量 |

### 6.3 使用流程

1. **初始化**：
   - 为初始主用节点添加 `node-role.kubernetes.io/role=primary` 标签
   - 备用节点保持无标签状态

2. **配置Pod**：
   - 为所有Pod添加nodeAffinity规则，确保只调度到主用节点

3. **部署工具**：
   - 部署本工具到集群中
   - 启用多可用区支持：
     ```bash
     nodecontroller auto --multiaz --interval=1m --threshold=75
     ```
   - 工具会自动开始监控节点状态和可用区分布

4. **自动管理**：
   - 工具会自动处理节点故障、资源过载和节点恢复
   - 无需手动干预节点角色切换

## 7. 监控与日志

### 7.1 监控指标
- 主用节点数量
- 备用节点数量
- 故障节点数量
- 过载节点数量
- 故障转移次数
- 节点升级次数
- 节点降级次数

### 7.2 日志输出
工具会输出详细的日志信息，包括：
- 节点状态变化
- 故障转移操作
- 资源使用率警告
- 节点升级和降级操作
- 错误信息

## 8. 优势与限制

### 8.1 优势
- **简化配置**：通过标签管理替代污点和容忍度，减少配置复杂度
- **自动化**：自动检测节点状态并处理故障转移
- **智能资源管理**：基于资源使用率动态调整节点状态
- **高可用性**：确保主用节点故障时能够快速切换到备用节点
- **灵活性**：可以根据实际需求调整备用节点数量和资源阈值

### 8.2 限制
- **依赖Kubernetes API**：需要足够的权限来管理节点标签和污点
- **Pod配置要求**：需要为所有Pod添加nodeAffinity规则
- **资源检测延迟**：资源使用率检测可能存在一定延迟
- **集群规模限制**：在超大规模集群中，可能需要调整检测间隔以避免API服务器过载

## 9. 总结

本设计方案通过动态管理节点标签和污点，实现了一种智能的主备节点规划方案，能够自动处理节点故障、资源过载和节点恢复等场景。该方案简化了配置复杂度，提高了集群的可用性和资源利用率，为大规模Kubernetes集群的节点管理提供了一种有效的解决方案。

通过定期检测节点状态、智能选择备用节点进行升级、合理管理资源使用率和处理节点恢复等操作，该方案能够确保服务的持续可用性和资源的合理分配，为集群的稳定运行提供有力保障。