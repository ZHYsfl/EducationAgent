from __future__ import annotations

from typing import Any, Optional

from pydantic import BaseModel, Field


class APIResponse(BaseModel):
    code: int
    message: Optional[str] = None
    data: Any = None


class InitTeachingElements(BaseModel):
    knowledge_points: list[str] = Field(default_factory=list)
    teaching_goals: list[str] = Field(default_factory=list)
    teaching_logic: str = ""
    key_difficulties: list[str] = Field(default_factory=list)
    duration: str = ""
    interaction_design: str = ""
    output_formats: list[str] = Field(default_factory=list)


class ReferenceFile(BaseModel):
    file_id: str
    file_url: str
    file_type: str  # pdf | docx | pptx | image | video
    instruction: str = ""


class PPTInitRequest(BaseModel):
    user_id: str
    topic: str
    description: str
    total_pages: int = 0
    audience: str = ""
    global_style: str = ""
    session_id: str
    teaching_elements: Optional[InitTeachingElements] = None
    reference_files: list[ReferenceFile] = Field(default_factory=list)


class Intent(BaseModel):
    action_type: str  # modify | insert_before | insert_after | delete | global_modify | resolve_conflict
    target_page_id: str
    instruction: str


class PPTFeedbackRequest(BaseModel):
    task_id: str
    base_timestamp: int
    viewing_page_id: str
    reply_to_context_id: str = ""
    raw_text: str = ""
    intents: list[Intent] = Field(default_factory=list)


class PPTExportRequest(BaseModel):
    task_id: str
    format: str  # pptx | docx | html5


class CanvasPageStatusInfo(BaseModel):
    page_id: str
    status: str  # rendering | completed | failed | suspended_for_human
    last_update: int
    render_url: str = ""


class CanvasStatusResponse(BaseModel):
    task_id: str
    page_order: list[str]
    current_viewing_page_id: str = ""
    pages_info: list[CanvasPageStatusInfo]


class PageRenderResponse(BaseModel):
    page_id: str
    task_id: str
    status: str
    render_url: str
    py_code: str  # §3.7：当前页面的 Python 源码（内含 get_slide_markup 返回 HTML）
    version: int
    updated_at: int


class CanvasRenderExecuteRequest(BaseModel):
    """§3.8.3 CanvasRenderer：执行单页渲染任务。"""

    task_id: str
    page_id: str
    py_code: str = ""  # 空则使用任务内当前 py_code


class CanvasRenderExecuteResponseData(BaseModel):
    success: bool
    error: str = ""
    render_url: str = ""


class VADEventRequest(BaseModel):
    """§3.1.1 VAD 极速触发信号（Voice 侧转发或由网关调用 PPT Agent）。"""

    task_id: str
    timestamp: int  # Unix ms，T_vad
    viewing_page_id: str = ""  # 用户开口时屏幕停留的页面 ID


class VADSnapshotResponseData(BaseModel):
    """VAD 成功时仅返回 accepted（与规范一致，不暴露 Redis key）。"""

    accepted: bool = True


class PPTInitResponseData(BaseModel):
    task_id: str


class PPTExportInitResponseData(BaseModel):
    export_id: str
    status: str
    estimated_seconds: int = 0


class PPTExportStatusData(BaseModel):
    export_id: str
    status: str
    download_url: str = ""
    format: str
    file_size: int = 0


# §7.3.11 Database Service — 任务（本服务内聚实现，供联调）
class CreateTaskRequest(BaseModel):
    session_id: str
    user_id: str
    topic: str
    description: str
    total_pages: int = 0
    audience: str = ""
    global_style: str = ""


class UpdateTaskStatusRequest(BaseModel):
    status: str  # pending | generating | completed | failed | exporting


class TaskDetailData(BaseModel):
    task_id: str
    session_id: str
    user_id: str
    topic: str
    description: str
    total_pages: int
    audience: str
    global_style: str
    status: str
    version: int
    last_update: int
    current_viewing_page_id: str = ""
    page_order: list[str] = Field(default_factory=list)


class TaskListItem(BaseModel):
    task_id: str
    session_id: str
    user_id: str
    topic: str
    status: str
    last_update: int


class TaskListData(BaseModel):
    total: int
    page: int
    page_size: int
    items: list[TaskListItem]


# §7.3.7–7.3.10 Sessions
class CreateSessionRequest(BaseModel):
    user_id: str
    title: str = ""


class CreateSessionResponseData(BaseModel):
    session_id: str


class SessionDetailData(BaseModel):
    session_id: str
    user_id: str
    title: str = ""
    status: str = "active"
    created_at: int = 0
    updated_at: int = 0


class SessionListItem(BaseModel):
    session_id: str
    user_id: str
    title: str = ""
    status: str = "active"
    created_at: int = 0
    updated_at: int = 0


class SessionListData(BaseModel):
    total: int
    page: int
    page_size: int
    items: list[SessionListItem]


class UpdateSessionRequest(BaseModel):
    title: Optional[str] = None
    status: Optional[str] = None  # active | completed | archived

