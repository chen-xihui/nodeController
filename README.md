# Node Controller Tool

## 项目概述

Node Controller是一个自动化的Kubernetes节点管理工具，用于实现主备节点规划和自动故障转移。该工具通过Kubernetes的标签和nodeAffinity机制，实现节点的主备角色管理，并提供自动监控和故障检测功能。工具支持多可用区部署，节点升级和降级时只考虑同一可用区，避免跨可用区迁移，提高了集群的高可用性和容错能力。

## 核心功能

### 1. 自动模式 (Auto Mode)
- **自动监控**：定期检查节点健康状态和资源使用情况
- **自动故障检测**：检测主用节点故障并触发故障转移
- **资源阈值管理**：当主用节点资源使用超过阈值时，自动允许Pod调度到备用节点
- **自动配置修复**：检测并修复节点的标签和污点配置

### 2. 节点管理
- **标签管理**：为节点添加主用/备用角色标签
- **污点管理**：为节点设置资源过载污点
- **状态查看**：查看节点的详细状态，包括标签、污点和资源使用情况
- **多可用区支持**：支持将节点按可用区划分，节点升级和降级时只考虑同一可用区

### 3. 监控模式 (Monitor Mode)
- **持续监控**：定期显示节点状态和资源使用情况
- **实时反馈**：及时显示节点状态变化

### 4. 多可用区支持
- **可用区划分**：支持将节点按可用区划分（如 `region-name.az01arm`、`region-name.az02arm`、`region-name.az03arm`）
- **同可用区操作**：节点升级和降级时只考虑同一可用区，避免跨可用区迁移
- **数量平衡**：每个可用区的主用节点和备用节点数量保持独立平衡
- **高可用性**：通过可用区内的节点平衡，提高每个可用区的容错能力

## 技术原理

### 标签机制
- **主用节点**：添加标签 `node-role.kubernetes.io/role=primary`
- **备用节点**：无标签

### NodeAffinity调度策略
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

### 调度策略
1. Pod只会被调度到带有 `node-role.kubernetes.io/role=primary` 标签的节点上
2. 当主用节点故障时，自动将同一可用区的备用节点升级为主用节点
3. 当主用节点资源使用率超过阈值时，添加NoSchedule污点防止新Pod调度
4. 备用节点升级为主用节点后，Pod可以调度到该节点

## 安装与编译

### 前提条件
- Go 1.20或更高版本
- Kubernetes集群访问权限（kubectl配置正确）
- kubectl命令行工具
- kubectl top命令支持（metrics-server安装）

### 编译步骤

1. 克隆或下载项目到本地
2. 进入项目目录
3. 执行编译命令：

```bash
go build -o nodecontroller.exe -mod=mod ./cmd/nodecontroller
```

4. 添加到系统PATH（可选）

## 使用方法

### 1. 自动模式

自动模式是最核心的功能，它会定期检查节点状态并自动处理故障转移：

```bash
# 基本用法
nodecontroller auto

# 自定义检查间隔和资源阈值
nodecontroller auto --interval=1m --threshold=75

# 启用多可用区支持
nodecontroller auto --multiaz

# 指定kubeconfig文件
nodecontroller auto --kubeconfig=/path/to/kubeconfig
```

**参数说明**：
- `--interval`：检查间隔，默认30秒
- `--threshold`：资源使用阈值百分比，默认90%
- `--multiaz`：启用多可用区支持，默认false
- `--kubeconfig`：kubeconfig文件路径

### 2. 标签管理

为节点添加主用/备用角色标签：

```bash
# 标记为主用节点
nodecontroller label node1 node2 --role=primary

# 标记为备用节点
nodecontroller label node3 node4 --role=backup
```

### 3. 污点管理

为节点设置相应的污点配置：

```bash
# 设置主用节点污点（移除污点）
nodecontroller taint node1 node2 --role=primary

# 设置备用节点污点
nodecontroller taint node3 node4 --role=backup
```

### 4. 状态查看

查看节点的详细状态：

```bash
# 查看所有节点状态
nodecontroller status

# 查看指定节点状态
nodecontroller status node1 node2
```

### 5. 监控模式

持续监控节点状态：

```bash
# 基本监控
nodecontroller monitor

# 自定义监控间隔
nodecontroller monitor --interval=30s
```

### 6. 查看帮助

```bash
nodecontroller help
```

## 自动模式工作流程

1. **初始化**：加载Kubernetes配置，设置检查间隔，启用多可用区支持（如果指定）
2. **节点发现**：获取集群中所有节点
3. **节点分类**：根据标签将节点分为主用节点、备用节点和未标记节点，按可用区分组
4. **状态检查**：
   - 检查节点健康状态（Ready条件）
   - 检查节点资源使用情况（CPU和内存）
   - 检查节点标签和污点配置
   - 检查节点所属的可用区
5. **故障处理**：
   - 当主用节点故障时，检查同一可用区的可用主用节点数量，若不足则升级同一可用区的备用节点
   - 当主用节点资源使用超过阈值时，添加NoSchedule污点，检查同一可用区的可用主用节点数量，若不足则升级同一可用区的备用节点
   - 当节点配置不正确时，自动修复配置
6. **节点恢复处理**：
   - 当故障节点恢复时，检查同一可用区的备用节点数量，必要时降级升级的备用节点
   - 确保每个可用区的备用节点数量保持平衡
7. **循环检查**：按照设定的间隔重复上述步骤

## 多可用区支持

### 概述
Node Controller支持多可用区部署，将集群节点按可用区划分，节点升级和降级时只考虑同一可用区，避免跨可用区迁移。

### 可用区标签
为每个节点添加可用区标签：
```bash
kubectl label nodes <node-name> topology.kubernetes.io/zone=region-name.az01arm
kubectl label nodes <node-name> topology.kubernetes.io/zone=region-name.az02arm
kubectl label nodes <node-name> topology.kubernetes.io/zone=region-name.az03arm
```

### 多可用区策略
1. **同可用区操作**：节点升级和降级时优先考虑同一可用区，不跨可用区操作
2. **数量平衡**：每个可用区的主用节点和备用节点数量保持独立平衡
3. **资源优化**：在同一可用区内，选择资源最充足的节点进行升级
4. **高可用性**：通过可用区内的节点平衡，提高每个可用区的容错能力
5. **兜底策略**：当同一可用区没有可用备用节点时，从备用节点数量最多的其他可用区选择节点进行升级

### 启用多可用区支持
```bash
nodecontroller auto --multiaz
```

### 多可用区工作原理
- 当某个可用区的主用节点故障时，优先从同一可用区的备用节点中选择节点进行升级
- 当同一可用区没有可用备用节点时，从备用节点数量最多的其他可用区选择节点进行升级
- 当某个可用区的备用节点数量不足时，优先降级该可用区中已升级的备用节点
- 避免跨可用区的节点迁移，减少网络延迟和复杂性（仅在必要时进行跨可用区升级）

## Pod配置要求

为了支持Pod在主用节点故障时自动调度到备用节点，所有Pod需要添加以下nodeAffinity配置：

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

### 示例Deployment配置

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: example-app
spec:
  replicas: 3
  selector:
    matchLabels:
      app: example
  template:
    metadata:
      labels:
        app: example
    spec:
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
            - matchExpressions:
              - key: node-role.kubernetes.io/role
                operator: In
                values:
                - primary
      containers:
      - name: app
        image: nginx:latest
        ports:
        - containerPort: 80
```

## 实施步骤

### 1. 准备工作
- 确保kubectl命令行工具已配置并可访问集群
- 确保metrics-server已安装（用于资源使用监控）
- 编译nodecontroller工具

### 2. 初始配置
- 标记主用节点：`nodecontroller label <nodes> --role=primary`
- 备用节点保持无标签状态
- 为所有节点添加可用区标签：`kubectl label nodes <node-name> topology.kubernetes.io/zone=region-name.az01arm`
- 为Pod添加nodeAffinity配置

### 3. 启动自动模式
```bash
# 基本模式
nodecontroller auto --interval=1m --threshold=90

# 启用多可用区支持
nodecontroller auto --multiaz --interval=1m --threshold=90
```

### 4. 验证配置
- 查看节点状态：`nodecontroller status`
- 创建测试Pod并检查调度情况
- 模拟主用节点故障，检查备用节点是否自动升级
- 验证Pod是否只调度到带有 `node-role.kubernetes.io/role=primary` 标签的节点

## 监控与维护

### 日志管理
- 自动模式会在控制台输出详细的监控信息
- 可通过重定向将输出保存到日志文件：`nodecontroller auto > nodecontroller.log 2>&1`

### 定期检查
- 建议定期查看节点状态，确保配置正确
- 定期检查自动模式的运行状态，确保监控正常

### 故障处理
- 当主用节点恢复后，Pod不会自动迁移回主用节点
- 如需将Pod迁移回主用节点，可使用kubectl drain命令

## 注意事项

1. **兼容性**：本工具不影响现有Pod运行，但新Pod需要添加nodeAffinity配置
2. **性能**：标签和污点操作对集群性能影响极小
3. **维护**：在节点扩容时，需及时标记新节点的角色和可用区标签
4. **依赖**：工具依赖kubectl命令行工具和metrics-server
5. **权限**：执行工具的用户需要有节点管理权限
6. **多可用区**：启用多可用区支持时，确保每个可用区的节点数量相同，并正确设置可用区标签

## 故障排查

### 常见问题

1. **工具无法连接到集群**
   - 检查kubectl配置是否正确
   - 检查集群访问权限

2. **自动模式无法检测节点状态**
   - 检查metrics-server是否正常运行
   - 检查节点是否启用了资源指标

3. **Pod无法调度到备用节点**
   - 检查Pod是否添加了正确的nodeAffinity配置
   - 检查备用节点是否已升级为主用节点（添加了 `node-role.kubernetes.io/role=primary` 标签）
   - 检查备用节点是否就绪（Ready状态）

4. **资源阈值检测不准确**
   - 检查metrics-server是否正常收集资源使用数据
   - 考虑调整资源阈值设置

### 调试方法
- 使用`nodecontroller status`查看节点详细状态
- 检查工具输出的详细日志信息
- 使用kubectl命令验证节点配置：`kubectl get nodes --show-labels`

## 总结

Node Controller工具通过自动化的方式实现了Kubernetes集群的主备节点规划和故障转移。它利用Kubernetes原生的标签和nodeAffinity机制，结合自动监控和故障检测功能，为集群提供了更可靠的节点管理方案。该工具支持多可用区部署，节点升级和降级时只考虑同一可用区，避免跨可用区迁移，提高了集群的高可用性和容错能力。该工具操作简单，影响最小，是大规模Kubernetes集群节点管理的理想选择。