# 5分支合并指南

## 合并顺序（从少到多）

| 顺序 | 分支 | 提交数 | 主要内容 |
|------|------|--------|----------|
| 1 | **zty** | 3 | PPTAgent |
| 2 | **wang** | 11 | 召回函数 |
| 3 | **zcxppt** | 11 | Intents解析 |
| 4 | **dyp** | 13 | kb-service |

---

## 步骤1：准备

```bash
# 切到main，强制覆盖本地未跟踪文件
git checkout -f main

# 确保main最新
git pull origin main
```

---

## 步骤2：合并第1个分支 (zty)

```bash
git merge zty --no-edit
```

**结果判断：**

✅ **成功** → 继续步骤3

❌ **有冲突** → 看VS Code的"源代码管理"面板：
- 红色 = 有冲突的文件
- 打开文件 → 找 `<<<<<<<` 标记
- 选择：保留当前 / 保留传入 / 两者都保留
- 解决完 → `git add .`
- `git commit -m "Merge zty with conflict resolution"`
- 然后继续步骤3

---

## 步骤3：合并第2个分支 (wang)

```bash
git merge wang --no-edit
```

有冲突 → 同上解决 → 然后继续

---

## 步骤4：合并第3个分支 (zcxppt)

```bash
git merge zcxppt --no-edit
```

---

## 步骤5：合并第4个分支 (dyp)

```bash
git merge dyp --no-edit
```

---

## 步骤6：验证

```bash
# 查看合并后的提交历史
git log --oneline --graph -20

# 查看所有分支是否都包含进来了
git branch -vv
```

---

## 冲突解决示例

如果看到：
```
Auto-merging voice_agent/agent/pipeline.go
CONFLICT (content): Merge conflict in voice_agent/agent/pipeline.go
Automatic merge failed; fix conflicts and then commit the result.
```

**VS Code操作：**
1. 打开 `pipeline.go`
2. 找到冲突标记：
```go
<<<<<<< HEAD  (当前main的代码)
func process() {
    version1()
}
=======       (wang分支的代码)
func process() {
    version2()
}
>>>>>>> wang
```
3. 修改成你想要的，删除 `<<<<<<<` `=======` `>>>>>>>` 标记
4. 保存文件
5. `git add voice_agent/agent/pipeline.go`
6. `git commit -m "Resolve conflict in pipeline.go"`
7. 继续合并下一个分支

---

## 中途放弃

如果想放弃合并：
```bash
git merge --abort
git checkout -f main  # 回到干净状态
```
