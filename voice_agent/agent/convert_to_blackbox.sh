#!/bin/bash
# convert_to_blackbox.sh - 转换白盒测试为黑盒测试

set -e

TEST_DIR="test"

# 创建 test 目录
mkdir -p "$TEST_DIR"

# 复制所有测试文件到 test 目录
for file in *_test.go; do
    cp "$file" "$TEST_DIR/"
done

# 转换每个测试文件
for file in "$TEST_DIR"/*_test.go; do
    echo "Converting: $file"

    # 1. 将 package agent 改为 package agent_test
    sed -i 's/^package agent$/package agent_test/' "$file"

    # 2. 添加 agent 导入（如果没有的话）
    # 检查是否已有 voiceagent/agent 导入
    if ! grep -q '"voiceagent/agent"' "$file"; then
        # 在 import 语句中添加 agent 导入
        sed -i 's/import (/import (\n\tagent "voiceagent\/agent"/' "$file"
    fi

    # 3. 替换类型引用（需要加 agent. 前缀）
    # 注意：只替换类型，不替换标准库类型

    # Session, Pipeline, MockServices 等
    sed -i 's/\b&MockServices{/\&agent.MockServices{/g' "$file"
    sed -i 's/\bMockServices{/agent.MockServices{/g' "$file"
    sed -i 's/\bNewTestSession(/agent.NewTestSession(/g' "$file"
    sed -i 's/\bNewTestPipeline(/agent.NewTestPipeline(/g' "$file"
    sed -i 's/\bNewTestPipelineWithTTS(/agent.NewTestPipelineWithTTS(/g' "$file"
    sed -i 's/\bNewTestConfig(/agent.NewTestConfig(/g' "$file"
    sed -i 's/\bNewSession(/agent.NewSession(/g' "$file"
    sed -i 's/\bNewPipeline(/agent.NewPipeline(/g' "$file"

    # 常量和类型
    sed -i 's/\bStateIdle\b/agent.StateIdle/g' "$file"
    sed -i 's/\bStateListening\b/agent.StateListening/g' "$file"
    sed -i 's/\bStateProcessing\b/agent.StateProcessing/g' "$file"
    sed -i 's/\bStateSpeaking\b/agent.StateSpeaking/g' "$file"
    sed -i 's/\bWSMessage{/agent.WSMessage{/g' "$file"
    sed -i 's/\bContextMessage{/agent.ContextMessage{/g' "$file"
    sed -i 's/\bContextMessage(/agent.ContextMessage(/g' "$file"
    sed -i 's/\bAPIResponse{/agent.APIResponse{/g' "$file"
    sed -i 's/\bAPIResponse(/agent.APIResponse(/g' "$file"
    sed -i 's/\bKBQueryRequest{/agent.KBQueryRequest{/g' "$file"
    sed -i 's/\bKBQueryResponse{/agent.KBQueryResponse{/g' "$file"
    sed -i 's/\bMemoryRecallRequest{/agent.MemoryRecallRequest{/g' "$file"
    sed -i 's/\bMemoryRecallResponse{/agent.MemoryRecallResponse{/g' "$file"
    sed -i 's/\bUserProfile{/agent.UserProfile{/g' "$file"
    sed -i 's/\bSearchRequest{/agent.SearchRequest{/g' "$file"
    sed -i 's/\bSearchResponse{/agent.SearchResponse{/g' "$file"
    sed -i 's/\bPPTInitRequest{/agent.PPTInitRequest{/g' "$file"
    sed -i 's/\bPPTInitResponse{/agent.PPTInitResponse{/g' "$file"
    sed -i 's/\bPPTFeedbackRequest{/agent.PPTFeedbackRequest{/g' "$file"
    sed -i 's/\bCanvasStatusResponse{/agent.CanvasStatusResponse{/g' "$file"
    sed -i 's/\bPageStatusInfo{/agent.PageStatusInfo{/g' "$file"
    sed -i 's/\bIngestFromSearchRequest{/agent.IngestFromSearchRequest{/g' "$file"
    sed -i 's/\bMemoryExtractRequest{/agent.MemoryExtractRequest{/g' "$file"
    sed -i 's/\bMemoryExtractResponse{/agent.MemoryExtractResponse{/g' "$file"
    sed -i 's/\bWorkingMemorySaveRequest{/agent.WorkingMemorySaveRequest{/g' "$file"
    sed -i 's/\bWorkingMemory{/agent.WorkingMemory{/g' "$file"
    sed -i 's/\bVADEvent{/agent.VADEvent{/g' "$file"
    sed -i 's/\bTaskRequirements{/agent.TaskRequirements{/g' "$file"
    sed -i 's/\bNewTaskRequirements(/agent.NewTaskRequirements(/g' "$file"
    sed -i 's/\bRetrievedChunk{/agent.RetrievedChunk{/g' "$file"
    sed -i 's/\bReferenceFile{/agent.ReferenceFile{/g' "$file"
    sed -i 's/\bReferenceFileReq{/agent.ReferenceFileReq{/g' "$file"

    # 函数调用
    sed -i 's/\bRegisterSession(/agent.RegisterSession(/g' "$file"
    sed -i 's/\bUnregisterSession(/agent.UnregisterSession(/g' "$file"
    sed -i 's/\bRegisterTask(/agent.RegisterTask(/g' "$file"
    sed -i 's/\bUnregisterTask(/agent.UnregisterTask(/g' "$file"
    sed -i 's/\bFindSessionByID(/agent.FindSessionByID(/g' "$file"
    sed -i 's/\bFindSessionByTaskID(/agent.FindSessionByTaskID(/g' "$file"
    sed -i 's/\bSetGlobalClients(/agent.SetGlobalClients(/g' "$file"
    sed -i 's/\bHandlePreview(/agent.HandlePreview(/g' "$file"
    sed -i 's/\bHandleUpload(/agent.HandleUpload(/g' "$file"
    sed -i 's/\bHandlePPTMessage(/agent.HandlePPTMessage(/g' "$file"
    sed -i 's/\bDrainWriteCh(/agent.DrainWriteCh(/g' "$file"
    sed -i 's/\bFindWSMessage(/agent.FindWSMessage(/g' "$file"
    sed -i 's/\bWaitForFeedback(/agent.WaitForFeedback(/g' "$file"
    sed -i 's/\bWaitForExtractMem(/agent.WaitForExtractMem(/g' "$file"
    sed -i 's/\bIsSentenceEnd(/agent.IsSentenceEnd(/g' "$file"
    sed -i 's/\bTruncate(/agent.Truncate(/g' "$file"
    sed -i 's/\bFormatProfileSummary(/agent.FormatProfileSummary(/g' "$file"
    sed -i 's/\bFormatChunksForLLM(/agent.FormatChunksForLLM(/g' "$file"
    sed -i 's/\bFormatMemoryForLLM(/agent.FormatMemoryForLLM(/g' "$file"
    sed -i 's/\bFormatSearchForLLM(/agent.FormatSearchForLLM(/g' "$file"
    sed -i 's/\bDecodeAPIData(/agent.DecodeAPIData(/g' "$file"
    sed -i 's/\bToReferenceFiles(/agent.ToReferenceFiles(/g' "$file"
    sed -i 's/\bBuildDetailedDescription(/agent.BuildDetailedDescription(/g' "$file"
    sed -i 's/\bIsInterrupt(/agent.IsInterrupt(/g' "$file"

    # MockTTS 类型
    sed -i 's/\b&MockTTS{/\&agent.MockTTS{/g' "$file"
    sed -i 's/\bMockTTS{/agent.MockTTS{/g' "$file"

    # ExternalServices 接口
    sed -i 's/\bExternalServices{/agent.ExternalServices{/g' "$file"

done

echo "Conversion complete!"
