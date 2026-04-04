#!/bin/bash
# 合并5个分支到main的脚本

echo "=== 开始合并5个分支到main ==="
echo ""

# 1. 先切到main
echo "1. 切换到main分支..."
git checkout main || exit 1
git pull origin main 2>/dev/null || echo "已是最新"
echo ""

# 2. 依次合并其他分支
echo "2. 开始合并其他分支..."
echo ""

BRANCHES="zty dyp zcxppt wang"

for branch in $BRANCHES; do
    echo "========================================="
    echo "正在合并: $branch"
    echo "========================================="

    git merge $branch --no-edit 2>&1

    if [ $? -ne 0 ]; then
        echo ""
        echo "⚠️  $branch 合并有冲突！"
        echo "请解决冲突后，执行:"
        echo "  git add ."
        echo "  git commit -m \"Merge $branch with conflict resolution\""
        echo "  然后重新运行此脚本"
        exit 1
    fi

    echo "✅ $branch 合并成功"
    echo ""
done

echo "========================================="
echo "✅ 所有分支合并完成！"
echo "========================================="
echo ""
echo "当前分支状态:"
git branch -vv
