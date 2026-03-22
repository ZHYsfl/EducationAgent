from __future__ import annotations

import asyncio
import logging
import os
import shutil
from contextlib import asynccontextmanager
from pathlib import Path
from typing import Any, Optional

from fastapi import FastAPI, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse
from fastapi.staticfiles import StaticFiles
from starlette.exceptions import HTTPException as StarletteHTTPException

from ppt_agent_service.ppt_generator import PPTGenerator
import ppt_agent_service.feedback_pipeline as feedback_pipeline

from ppt_agent_service.canvas_redis import canvas_store, task_to_canvas_document
from ppt_agent_service.lesson_plan_docx import build_lesson_plan_docx
from ppt_agent_service.slide_py_code import (
    extract_slide_html_from_py,
    wrap_slide_html_as_python,
)
from ppt_agent_service.db.http_repository import HTTPPPTRepository
from ppt_agent_service.db_service_proxy import proxy_sessions_to_database_service
from ppt_agent_service.api_http_utils import api_error, assert_body_user_matches_jwt
from ppt_agent_service.schemas import (
    APIResponse,
    CanvasPageStatusInfo,
    CanvasRenderExecuteRequest,
    CanvasRenderExecuteResponseData,
    CanvasStatusResponse,
    CreateSessionRequest,
    CreateTaskRequest,
    PPTExportInitResponseData,
    PPTExportRequest,
    PPTFeedbackRequest,
    PPTInitRequest,
    PPTInitResponseData,
    TaskDetailData,
    TaskListData,
    TaskListItem,
    UpdateSessionRequest,
    UpdateTaskStatusRequest,
    VADEventRequest,
    VADSnapshotResponseData,
)
from ppt_agent_service.auth import AuthContext, is_auth_enforced
from ppt_agent_service.auth_middleware import PPTAuthMiddleware
from ppt_agent_service import error_codes as ec
from ppt_agent_service.task_manager import (
    ExportState,
    PageState,
    TaskState,
    TaskManager,
    utc_ms,
)

logger = logging.getLogger(__name__)


REPO_ROOT = Path(__file__).resolve().parents[1]
RUNS_DIR = REPO_ROOT / "runs"
STATIC_SERVE_DIR = RUNS_DIR

ALLOWED_INTENT_ACTIONS = frozenset(
    {
        "modify",
        "insert_before",
        "insert_after",
        "delete",
        "global_modify",
        "resolve_conflict",
    }
)
ALLOWED_TASK_STATUSES = frozenset(
    {"pending", "generating", "completed", "failed", "exporting"}
)


def public_media_url(path: str) -> str:
    """§3.5 / §3.7：相对静态路径可通过 PPT_PUBLIC_BASE_URL 拼成公网绝对 URL。"""
    if not path:
        return path
    p = str(path).strip()
    if p.startswith("http://") or p.startswith("https://"):
        return p
    base = os.getenv("PPT_PUBLIC_BASE_URL", "").rstrip("/")
    if not base:
        return p
    if not p.startswith("/"):
        p = "/" + p
    return base + p


def assert_task_access(request: Request, task: Optional[TaskState]) -> Optional[JSONResponse]:
    """JWT 用户仅能访问本人任务；X-Internal-Key 可访问任意任务。"""
    if task is None or not is_auth_enforced():
        return None
    ctx = getattr(request.state, "auth", None)
    if not isinstance(ctx, AuthContext) or ctx.is_internal:
        return None
    if not ctx.user_id:
        return None
    if task.user_id != ctx.user_id:
        return api_error(ec.EC_AUTH_FORBIDDEN_TASK, "无权访问该任务")
    return None


async def ensure_redis_or_api_error() -> Optional[JSONResponse]:
    """§0.2 502xx：强依赖 Redis 的接口在调用前检查。"""
    reason = await canvas_store.redis_unavailable_reason()
    if reason:
        return api_error(ec.EC_DEPENDENCY, f"依赖服务不可用（Redis）：{reason}")
    return None


@asynccontextmanager
async def _lifespan(app: FastAPI):
    await canvas_store.connect()
    remote = (os.getenv("PPT_DATABASE_SERVICE_URL") or "").strip().rstrip("/")
    if not remote:
        raise RuntimeError(
            "PPT Agent 不内嵌数据库：必须先启动 Database Service（§0.7，默认端口 9500，"
            "`python -m database_service`），并设置环境变量 "
            "PPT_DATABASE_SERVICE_URL（例如 http://127.0.0.1:9500）。"
        )
    app.state.ppt_remote_db_url = remote
    ik = (os.getenv("INTERNAL_KEY") or "").strip()
    task_manager.set_repository(HTTPPPTRepository(remote, ik))
    yield
    await task_manager.cancel_all_suspend_watchers()
    task_manager.set_repository(None)
    await canvas_store.close()


app = FastAPI(
    title="EducationAgent PPT Agent (HTTP Adapter)",
    lifespan=_lifespan,
)
app.add_middleware(PPTAuthMiddleware)
app.mount(
    "/static/runs",
    StaticFiles(directory=str(STATIC_SERVE_DIR), html=False),
    name="static-runs",
)


task_manager = TaskManager(runs_dir=RUNS_DIR)
generator = PPTGenerator(template_name=os.getenv("PPTAGENT_TEMPLATE", "default"))


@app.exception_handler(RequestValidationError)
async def _validation_handler(request: Request, exc: RequestValidationError):
    errs = exc.errors()
    parts: list[str] = []
    for e in errs[:8]:
        loc = ".".join(str(x) for x in e.get("loc", ()))
        parts.append(f"{loc}: {e.get('msg', '')}")
    msg = "请求参数无效：" + ("; ".join(parts) if parts else str(exc))
    return api_error(ec.EC_PARAM, msg[:800])


@app.exception_handler(StarletteHTTPException)
async def _http_exception_handler(request: Request, exc: StarletteHTTPException):
    if exc.status_code == 404:
        return api_error(ec.EC_NOT_FOUND, str(exc.detail) or "资源不存在")
    return api_error(ec.EC_INTERNAL, str(exc.detail) or "请求处理失败")


@app.exception_handler(Exception)
async def _unhandled_exception_handler(request: Request, exc: Exception):
    logger.exception("未处理异常: %s", exc)
    return api_error(ec.EC_INTERNAL, "内部错误")


async def safe_notify_voice_agent_ppt_status(
    *, task_id: str, format: str = "pptx"
) -> None:
    """§3.4 导出完成 → Voice（ppt_status）。"""
    from ppt_agent_service.voice_client import voice_ppt_message

    await voice_ppt_message(
        task_id=task_id,
        page_id="",
        priority="normal",
        context_id="",
        tts_text=f"课件导出完成，请下载（{format}）",
        msg_type="ppt_status",
    )


def verify_user_id_allowed(user_id: str) -> Optional[str]:
    """
    §3.2：可选启用 user_id 校验（模拟 DB 存在性）。
    PPT_USER_VERIFY=allowlist 且配置 PPT_USER_ALLOWLIST 时，不在列表中返回错误文案。
    """
    mode = os.getenv("PPT_USER_VERIFY", "off").lower().strip()
    if mode != "allowlist":
        return None
    raw = os.getenv("PPT_USER_ALLOWLIST", "")
    allow = {x.strip() for x in raw.split(",") if x.strip()}
    if not allow:
        return None
    if user_id not in allow:
        return "user_id 不存在"
    return None


def build_effective_description_for_init(req: PPTInitRequest) -> str:
    """在已通过非空校验后，拼接参考资料与教学要素（不改变生成管线，仅丰富 description）。"""
    effective_description = req.description.strip()
    if req.reference_files:
        blocks = []
        for i, rf in enumerate(req.reference_files, start=1):
            blocks.append(
                f"【参考资料{i}：{rf.file_type}】\n"
                f"使用说明：{rf.instruction or ''}\n"
                f"file_url：{rf.file_url}\n"
                f"file_id：{rf.file_id}\n"
            )
        effective_description += "\n\n" + "\n".join(blocks)

    if req.teaching_elements:
        te = req.teaching_elements
        effective_description += (
            "\n\n【教学要素（结构化兜底）】\n"
            f"knowledge_points：{', '.join(te.knowledge_points)}\n"
            f"teaching_goals：{', '.join(te.teaching_goals)}\n"
            f"key_difficulties：{', '.join(te.key_difficulties)}\n"
            f"duration：{te.duration}\n"
            f"interaction_design：{te.interaction_design}\n"
            f"output_formats：{', '.join(te.output_formats)}\n"
        )
    return effective_description


def validate_feedback_intents(
    task: TaskState, req: PPTFeedbackRequest
) -> tuple[Optional[JSONResponse], list[str]]:
    """
    §3.3：校验 Intent 并生成写入 description 的修改行。
    返回 (error_response, lines)；lines 非空表示可接受。
    """
    if not req.intents:
        return api_error(ec.EC_PARAM, "intents 数组为空"), []

    lines: list[str] = []
    has_resolve = any(it.action_type == "resolve_conflict" for it in req.intents)
    if has_resolve and not (req.reply_to_context_id and str(req.reply_to_context_id).strip()):
        return api_error(ec.EC_PARAM, "resolve_conflict 必须携带非空的 reply_to_context_id"), []

    pages_known = bool(task.pages) and task.status == "completed"

    for it in req.intents:
        if it.action_type not in ALLOWED_INTENT_ACTIONS:
            return (
                api_error(
                    ec.EC_PARAM,
                    f"不支持的 action_type：{it.action_type}",
                ),
                [],
            )

        if it.action_type == "global_modify":
            if (it.target_page_id or "").strip().upper() != "ALL":
                return (
                    api_error(ec.EC_PARAM, "global_modify 要求 target_page_id 为 ALL"),
                    [],
                )

        if it.action_type == "resolve_conflict":
            ctx = req.reply_to_context_id.strip()
            line = (
                f"[action=resolve_conflict, target={it.target_page_id}, "
                f"context_id={ctx}] {it.instruction or ''}"
            )
            lines.append(line)
            continue

        tid = (it.target_page_id or "").strip()
        if pages_known and tid and tid.upper() != "ALL" and tid not in task.pages:
            return api_error(ec.EC_NOT_FOUND, "page_id 不存在"), []

        instr = (it.instruction or "").strip()
        if it.action_type == "delete" and not instr:
            instr = "（删除本页）"
        if not instr and it.action_type not in ("delete",):
            return (
                api_error(ec.EC_PARAM, f"intent 缺少有效 instruction：{it.action_type}"),
                [],
            )

        ctx = ""
        if req.reply_to_context_id and str(req.reply_to_context_id).strip():
            ctx = req.reply_to_context_id.strip()
        extra = f", reply_to_context_id={ctx}" if ctx else ""
        if req.raw_text and req.raw_text.strip():
            extra += f", raw_text={req.raw_text.strip()[:500]}"
        lines.append(
            f"[action={it.action_type}, target={it.target_page_id}{extra}] {instr}"
        )

    if not lines:
        return api_error(ec.EC_PARAM, "无法从 intents 中提取有效修改指令"), []

    return None, lines


def render_url_for_task(task_id: str, slide_index: int) -> str:
    # ppt_to_images 输出命名：slide_{i:04d}.jpg
    return f"/static/runs/{task_id}/renders/slide_{slide_index:04d}.jpg"


def build_canvas_status_response(
    task: TaskState, doc: Optional[dict[str, Any]] = None
) -> CanvasStatusResponse:
    """§3.5：优先使用 Redis 画布文档，与内存任务合并 render_url 等字段。"""
    if not doc:
        pages_info = []
        for pid in task.page_order:
            p = task.pages.get(pid)
            if not p:
                continue
            pages_info.append(
                CanvasPageStatusInfo(
                    page_id=p.page_id,
                    status=p.status,
                    last_update=p.updated_at or task.last_update,
                    render_url=public_media_url(p.render_url),
                )
            )
        return CanvasStatusResponse(
            task_id=task.task_id,
            page_order=list(task.page_order),
            current_viewing_page_id=task.current_viewing_page_id,
            pages_info=pages_info,
        )

    page_order = doc.get("page_order") or task.page_order
    if not isinstance(page_order, list):
        page_order = list(task.page_order)

    cur_vid = doc.get("current_viewing_page_id")
    if cur_vid is None or cur_vid == "":
        cur_vid = task.current_viewing_page_id or ""

    rd_pages = doc.get("pages") or {}
    page_display = doc.get("page_display") or {}
    pages_info = []
    for pid in page_order:
        if not isinstance(pid, str):
            continue
        rd = rd_pages.get(pid) or {}
        mem = task.pages.get(pid)
        disp = (
            page_display.get(pid)
            if isinstance(page_display, dict)
            else None
        ) or {}
        if not isinstance(disp, dict):
            disp = {}
        # 新结构：render_url/last_update 在 page_display；兼容旧文档写在 pages 内
        render_url = (mem.render_url if mem else "") or str(
            disp.get("render_url") or rd.get("render_url", "")
        )
        status = str(rd.get("status") or (mem.status if mem else "completed"))
        lu = int(
            disp.get("last_update")
            or rd.get("last_update")
            or (mem.updated_at if mem else task.last_update)
            or utc_ms()
        )
        pages_info.append(
            CanvasPageStatusInfo(
                page_id=pid,
                status=status,
                last_update=lu,
                render_url=public_media_url(render_url),
            )
        )

    return CanvasStatusResponse(
        task_id=task.task_id,
        page_order=list(page_order),
        current_viewing_page_id=cur_vid,
        pages_info=pages_info,
    )


async def generate_task_pages(task: TaskState) -> None:
    """
    根据 task.description 生成 PPT，并更新 task 的 pages 状态。
    """
    task_dir = task_manager.task_dir(task.task_id)
    task_dir.mkdir(parents=True, exist_ok=True)

    # 整册重跑前清空悬挂/冲突/合并队列（页 ID 将重建）
    await task_manager.clear_task_suspensions_and_conflicts(task)

    # 提示状态
    task.status = "generating"
    task.last_update = utc_ms()
    # §3.1：生成中 Redis 画布页状态统一为 rendering（内存仍保留上一轮页面直至替换完成）
    await canvas_store.save_canvas_from_task(task, page_status_override="rendering")

    deck_dir = task_dir
    try:
        deck = await generator.generate_deck(
            task_dir=deck_dir,
            user_id=task.user_id,
            topic=task.topic,
            description=task.description,
            total_pages=task.total_pages,
            audience=task.audience,
            global_style=task.global_style,
            session_id=task.session_id,
            teaching_elements=task.teaching_elements,
            reference_files=task.reference_files,
        )
    except Exception as e:
        task.status = "failed"
        task.last_update = utc_ms()
        await task_manager.persist_task(task.task_id)
        raise e

    # 初始化页面路由
    pages: dict[str, PageState] = {}
    page_order: list[str] = []
    for i, html in enumerate(deck.slide_html, start=1):
        page_id = task_manager.new_page_id()
        p = PageState(
            page_id=page_id,
            slide_index=i,
            status="completed",
            render_url=render_url_for_task(task.task_id, i),
            py_code=wrap_slide_html_as_python(
                html, page_id=page_id, slide_index=i
            ),
            version=task.version,
            updated_at=utc_ms(),
        )
        pages[page_id] = p
        page_order.append(page_id)

    task.page_order = page_order
    task.pages = pages
    task.current_viewing_page_id = page_order[0] if page_order else ""
    task.output_pptx_path = deck.pptx_path
    task.last_update = utc_ms()
    task.status = "completed"
    await canvas_store.save_canvas_from_task(task)
    await task_manager.persist_task(task.task_id)


async def generate_task_background(task_id: str) -> None:
    """
    后台生成；§3.9.2：若生成过程中收到反馈，先入队，本轮完成后合并并重跑，不中断当前生成。
    """
    while True:
        task = await task_manager.get_task(task_id)
        if not task:
            return
        task.bump_version()
        try:
            await generate_task_pages(task)
        except Exception:
            task.status = "failed"
            task.last_update = utc_ms()
            await task_manager.persist_task(task_id)
            return

        task = await task_manager.get_task(task_id)
        if not task or task.status != "completed":
            return

        pending = await task_manager.take_pending_feedback_lines(task_id)
        if not pending:
            return

        task.description = (
            task.description
            + "\n\n[教师反馈修改]\n"
            + "\n".join(pending)
            + "\n"
        )
        task.last_update = utc_ms()
        await task_manager.persist_task(task_id)


async def schedule_regeneration(task_id: str) -> None:
    """首次生成后仅用于：增删页等结构性变更、或局部编辑失败时的回退整册重跑。"""
    t = await task_manager.get_task(task_id)
    if not t:
        return
    if t.running_job is not None and not t.running_job.done():
        return
    t.status = "generating"
    t.running_job = asyncio.create_task(generate_task_background(task_id))
    await task_manager.persist_task(task_id)


async def schedule_partial_refresh(task_id: str, page_ids: list[str]) -> None:
    """局部改页后刷新预览图与 pptx（不调用 PPTAgent）。"""
    t = await task_manager.get_task(task_id)
    if not t or not page_ids:
        return
    await asyncio.to_thread(generator.refresh_slide_assets, t, page_ids)
    await canvas_store.save_canvas_from_task(t)
    await task_manager.persist_task(task_id)


feedback_pipeline.set_schedule_regeneration(schedule_regeneration)
feedback_pipeline.set_partial_refresh(schedule_partial_refresh)
feedback_pipeline.set_slide_editor(generator)


@app.post("/api/v1/ppt/init")
async def ppt_init(req: PPTInitRequest, request: Request):
    # §3.2 错误码：40001 topic/description 为空；40400 user_id 不存在（可选 allowlist）
    topic = (req.topic or "").strip()
    desc = (req.description or "").strip()
    uid = (req.user_id or "").strip()
    sid = (req.session_id or "").strip()
    if not topic:
        return api_error(ec.EC_PARAM, "参数 topic 不能为空")
    if not desc:
        return api_error(ec.EC_PARAM, "参数 description 不能为空")
    if not uid:
        return api_error(ec.EC_PARAM, "参数 user_id 不能为空")
    if not sid:
        return api_error(ec.EC_PARAM, "参数 session_id 不能为空")
    if req.total_pages < 0:
        return api_error(ec.EC_PARAM, "total_pages 不能为负")

    mismatch = assert_body_user_matches_jwt(request, uid)
    if mismatch is not None:
        return mismatch

    uerr = verify_user_id_allowed(uid)
    if uerr:
        return api_error(ec.EC_NOT_FOUND, uerr)

    await task_manager.ensure_user_session_db(uid, sid)

    effective_description = build_effective_description_for_init(req)

    task_id = task_manager.new_task_id()
    task = TaskState(
        task_id=task_id,
        user_id=uid,
        topic=topic,
        description=effective_description,
        total_pages=req.total_pages,
        audience=req.audience,
        global_style=req.global_style,
        session_id=sid,
        reference_files=[rf.model_dump() for rf in req.reference_files]
        if req.reference_files
        else [],
        teaching_elements=req.teaching_elements.model_dump()
        if req.teaching_elements
        else None,
    )

    await task_manager.upsert_task(task)

    await canvas_store.save_canvas_from_task(task)

    task.running_job = asyncio.create_task(generate_task_background(task_id))

    return JSONResponse(
        status_code=200,
        content=APIResponse(
            code=200,
            message="success",
            data=PPTInitResponseData(task_id=task_id).model_dump(),
        ).model_dump(),
    )


@app.post("/api/v1/ppt/vad")
@app.post("/api/v1/canvas/vad-event")
async def ppt_vad_event(req: VADEventRequest, request: Request):
    """
    §3.1.1 VAD：将当前画布写入 Redis 后深拷贝为 snapshot:{task_id}:{timestamp}（TTL 300s），
    并更新 current_viewing_page_id。

    **联调约定**：Voice/网关应在用户开口（vad_start）时调用本接口，以便 `ppt/feedback` 的
    `base_timestamp` 与快照对齐。若 Voice 侧改为直接写 Redis，需在对接文档中明确
    `canvas:{task_id}` / `snapshot:{task_id}:{ts}` 的 JSON 形状（与本服务 `task_to_canvas_document`
    / `to_strict_snapshot_payload` 一致）。
    """
    tid = (req.task_id or "").strip()
    if not tid:
        return api_error(ec.EC_PARAM, "参数 task_id 不能为空")
    if req.timestamp <= 0:
        return api_error(ec.EC_PARAM, "参数 timestamp 无效，应为 Unix 毫秒")

    task = await task_manager.get_task(tid)
    if not task:
        return api_error(ec.EC_NOT_FOUND, "task_id 不存在")

    denied = assert_task_access(request, task)
    if denied is not None:
        return denied

    redis_err = await ensure_redis_or_api_error()
    if redis_err is not None:
        return redis_err

    live_doc = task_to_canvas_document(task, ts=utc_ms())
    await canvas_store.save_canvas_document(tid, live_doc)
    try:
        await canvas_store.vad_deep_copy_snapshot(
            tid, int(req.timestamp), (req.viewing_page_id or "").strip(), live_doc
        )
    except RuntimeError as e:
        return api_error(ec.EC_DEPENDENCY, str(e))

    vid = (req.viewing_page_id or "").strip()
    if vid:
        task.current_viewing_page_id = vid
        task.last_update = utc_ms()
    await canvas_store.save_canvas_from_task(task)
    await task_manager.persist_task(tid)

    return JSONResponse(
        status_code=200,
        content=APIResponse(
            code=200,
            message="success",
            data=VADSnapshotResponseData(accepted=True).model_dump(),
        ).model_dump(),
    )


@app.post("/api/v1/ppt/feedback")
async def ppt_feedback(req: PPTFeedbackRequest, request: Request):
    task = await task_manager.get_task(req.task_id)
    if not task:
        return api_error(ec.EC_NOT_FOUND, "task_id 不存在")

    denied = assert_task_access(request, task)
    if denied is not None:
        return denied

    # §3.3：有效 viewing_page_id 时同步当前注视页并刷新 Redis 画布
    vid_fb = (req.viewing_page_id or "").strip()
    if vid_fb and vid_fb in task.pages:
        task.current_viewing_page_id = vid_fb
        task.last_update = utc_ms()
        await canvas_store.save_canvas_from_task(task)
        await task_manager.persist_task(req.task_id)

    err, lines = validate_feedback_intents(task, req)
    if err is not None:
        return err

    if task.status in ("failed", "exporting"):
        return api_error(ec.EC_CONFLICT, "任务已终止，不接受反馈")

    rc, rmsg = await feedback_pipeline.handle_resolve_conflict_branch(
        task_manager, canvas_store, task, req
    )
    if rc == "err":
        return api_error(ec.EC_PARAM, rmsg or "冲突处理失败")

    task = await task_manager.get_task(req.task_id) or task

    intents_merge = [it for it in req.intents if it.action_type != "resolve_conflict"]
    if not intents_merge:
        return JSONResponse(
            status_code=200,
            content=APIResponse(
                code=200,
                message="success",
                data={"accepted_intents": len(req.intents), "queued": rc == "ok"},
            ).model_dump(),
        )

    sub_req = PPTFeedbackRequest(
        task_id=req.task_id,
        base_timestamp=req.base_timestamp,
        viewing_page_id=req.viewing_page_id,
        reply_to_context_id="",
        raw_text=req.raw_text,
        intents=intents_merge,
    )
    err2, lines_merge = validate_feedback_intents(task, sub_req)
    if err2 is not None:
        return err2

    if task.status == "completed":
        if await feedback_pipeline.feedback_targets_suspended_page(task, intents_merge):
            await feedback_pipeline.queue_intents_on_suspended_and_reask(
                task_manager,
                canvas_store,
                task,
                intents_merge,
                raw_text=req.raw_text or "",
            )
            return JSONResponse(
                status_code=200,
                content=APIResponse(
                    code=200,
                    message="success",
                    data={
                        "accepted_intents": len(req.intents),
                        "queued": True,
                        "suspended_queued": True,
                    },
                ).model_dump(),
            )
        feedback_pipeline.start_merge_background(
            task_manager,
            canvas_store,
            task.task_id,
            intents_merge,
            req.base_timestamp,
            req.raw_text or "",
        )
        return JSONResponse(
            status_code=200,
            content=APIResponse(
                code=200,
                message="success",
                data={"accepted_intents": len(req.intents), "queued": True},
            ).model_dump(),
        )

    if task.status in ("pending", "generating"):
        await task_manager.append_pending_feedback(task.task_id, lines_merge)
        task.last_update = utc_ms()
        return JSONResponse(
            status_code=200,
            content=APIResponse(
                code=200,
                message="success",
                data={"accepted_intents": len(req.intents), "queued": True},
            ).model_dump(),
        )

    return api_error(ec.EC_CONFLICT, "任务状态不允许反馈")


@app.get("/api/v1/canvas/status")
async def canvas_status(task_id: str, request: Request):
    task = await task_manager.get_task(task_id)
    if not task:
        return api_error(ec.EC_NOT_FOUND, "task_id 不存在")

    denied = assert_task_access(request, task)
    if denied is not None:
        return denied

    doc = await canvas_store.load_canvas_document(task.task_id)
    resp = build_canvas_status_response(task, doc)

    return JSONResponse(
        status_code=200,
        content=APIResponse(code=200, message="success", data=resp.model_dump()).model_dump(),
    )


@app.get("/api/v1/tasks/{task_id}/preview")
async def tasks_preview(task_id: str, request: Request):
    """
    §2.3 前端预览：字段与系统接口规范示例一致（不含 progress）。
    """
    task = await task_manager.get_task(task_id)
    if not task:
        return api_error(ec.EC_NOT_FOUND, "task_id 不存在")

    denied = assert_task_access(request, task)
    if denied is not None:
        return denied

    pages = []
    for pid in task.page_order:
        p = task.pages.get(pid)
        if not p:
            continue
        pages.append(
            {
                "page_id": p.page_id,
                "status": p.status,
                "last_update": p.updated_at or task.last_update,
                "render_url": public_media_url(p.render_url),
            }
        )

    data = {
        "task_id": task.task_id,
        "status": task.status,
        "page_order": task.page_order,
        "current_viewing_page_id": task.current_viewing_page_id,
        "pages": pages,
    }
    return JSONResponse(
        status_code=200,
        content=APIResponse(code=200, message="success", data=data).model_dump(),
    )


@app.post("/api/v1/sessions")
async def create_session(req: CreateSessionRequest, request: Request):
    """§7.3.7 创建会话（转发至 Database Service）。"""
    return await proxy_sessions_to_database_service(request, json_body=req.model_dump())


@app.get("/api/v1/sessions/{session_id}")
async def get_session(session_id: str, request: Request):
    """§7.3.8 获取会话（转发至 Database Service）。"""
    return await proxy_sessions_to_database_service(request)


@app.get("/api/v1/sessions")
async def list_sessions(
    request: Request,
    user_id: str,
    page: int = 1,
    page_size: int = 20,
):
    """§7.3.9 列出用户会话（转发至 Database Service）。"""
    return await proxy_sessions_to_database_service(request)


@app.put("/api/v1/sessions/{session_id}")
async def update_session(
    session_id: str, body: UpdateSessionRequest, request: Request
):
    """§7.3.10 更新会话（转发至 Database Service）。"""
    return await proxy_sessions_to_database_service(
        request, json_body=body.model_dump(exclude_none=True)
    )


@app.post("/api/v1/tasks")
async def create_task(req: CreateTaskRequest, request: Request):
    """§7.3.11 创建任务（与 ppt/init 等价启动生成，无结构化教学要素/参考资料字段）。"""
    topic = (req.topic or "").strip()
    desc = (req.description or "").strip()
    uid = (req.user_id or "").strip()
    sid = (req.session_id or "").strip()
    if not topic:
        return api_error(ec.EC_PARAM, "参数 topic 不能为空")
    if not desc:
        return api_error(ec.EC_PARAM, "参数 description 不能为空")
    if not uid:
        return api_error(ec.EC_PARAM, "参数 user_id 不能为空")
    if not sid:
        return api_error(ec.EC_PARAM, "参数 session_id 不能为空")
    if req.total_pages < 0:
        return api_error(ec.EC_PARAM, "total_pages 不能为负")

    mismatch = assert_body_user_matches_jwt(request, uid)
    if mismatch is not None:
        return mismatch

    uerr = verify_user_id_allowed(uid)
    if uerr:
        return api_error(ec.EC_NOT_FOUND, uerr)

    await task_manager.ensure_user_session_db(uid, sid)

    task_id = task_manager.new_task_id()
    task = TaskState(
        task_id=task_id,
        user_id=uid,
        topic=topic,
        description=desc,
        total_pages=req.total_pages,
        audience=req.audience,
        global_style=req.global_style,
        session_id=sid,
    )
    await task_manager.upsert_task(task)
    await canvas_store.save_canvas_from_task(task)
    task.running_job = asyncio.create_task(generate_task_background(task_id))
    return JSONResponse(
        status_code=200,
        content=APIResponse(
            code=200,
            message="success",
            data={"task_id": task_id},
        ).model_dump(),
    )


@app.get("/api/v1/tasks/{task_id}")
async def get_task_detail(task_id: str, request: Request):
    """§7.3.12 获取任务。"""
    task = await task_manager.get_task(task_id)
    if not task:
        return api_error(ec.EC_NOT_FOUND, "task_id 不存在")

    denied = assert_task_access(request, task)
    if denied is not None:
        return denied

    data = TaskDetailData(
        task_id=task.task_id,
        session_id=task.session_id,
        user_id=task.user_id,
        topic=task.topic,
        description=task.description,
        total_pages=task.total_pages,
        audience=task.audience,
        global_style=task.global_style,
        status=task.status,
        version=task.version,
        last_update=task.last_update,
        current_viewing_page_id=task.current_viewing_page_id,
        page_order=list(task.page_order),
    )
    return JSONResponse(
        status_code=200,
        content=APIResponse(code=200, message="success", data=data.model_dump()).model_dump(),
    )


@app.put("/api/v1/tasks/{task_id}/status")
async def update_task_status(
    task_id: str, body: UpdateTaskStatusRequest, request: Request
):
    """§7.3.13 更新任务状态（内存态；非法值返回 40001）。"""
    task = await task_manager.get_task(task_id)
    if not task:
        return api_error(ec.EC_NOT_FOUND, "task_id 不存在")

    denied = assert_task_access(request, task)
    if denied is not None:
        return denied

    st = (body.status or "").strip()
    if st not in ALLOWED_TASK_STATUSES:
        return api_error(ec.EC_PARAM, f"非法 status，允许：{', '.join(sorted(ALLOWED_TASK_STATUSES))}")
    await task_manager.update_task(task_id, status=st, last_update=utc_ms())
    return JSONResponse(
        status_code=200,
        content=APIResponse(
            code=200,
            message="success",
            data={"task_id": task_id, "status": st},
        ).model_dump(),
    )


@app.get("/api/v1/tasks")
async def list_tasks(
    request: Request,
    session_id: str,
    page: int = 1,
    page_size: int = 20,
):
    """§7.3.14 列出会话下的任务（分页）。"""
    if not (session_id or "").strip():
        return api_error(ec.EC_PARAM, "缺少查询参数 session_id")
    uid_filter: Optional[str] = None
    if is_auth_enforced():
        ctx = getattr(request.state, "auth", None)
        if isinstance(ctx, AuthContext) and not ctx.is_internal and ctx.user_id:
            uid_filter = ctx.user_id.strip()
    items_raw, total = await task_manager.list_tasks_by_session_paged(
        session_id.strip(), page, page_size, user_id=uid_filter
    )
    items = [
        TaskListItem(
            task_id=t.task_id,
            session_id=t.session_id,
            user_id=t.user_id,
            topic=t.topic,
            status=t.status,
            last_update=t.last_update,
        )
        for t in items_raw
    ]
    p = max(1, page)
    ps = max(1, min(page_size, 100))
    data = TaskListData(total=total, page=p, page_size=ps, items=items).model_dump()
    return JSONResponse(
        status_code=200,
        content=APIResponse(code=200, message="success", data=data).model_dump(),
    )


async def export_task_background(export_id: str, task_id: str, fmt: str) -> None:
    exp = await task_manager.get_export(export_id)
    if not exp:
        return

    task = await task_manager.get_task(task_id)
    if not task:
        await task_manager.update_export(export_id, status="failed", last_update=utc_ms())
        return

    # 等待生成完成（如果还在生成）
    try:
        if task.running_job:
            await task.running_job
    except Exception:
        await task_manager.update_export(
            export_id, status="failed", last_update=utc_ms()
        )
        return

    task_dir = task_manager.task_dir(task_id)
    exports_dir = task_manager.export_dir(task_id)
    exports_dir.mkdir(parents=True, exist_ok=True)

    exp_file_path: Optional[Path] = None
    try:
        if fmt == "pptx":
            assert task.output_pptx_path is not None
            exp_file_path = exports_dir / f"{export_id}.pptx"
            shutil.copyfile(str(task.output_pptx_path), str(exp_file_path))
        elif fmt == "docx":
            # 教案：根据教学要素 + 任务描述 + 各页幻灯片文本生成 .docx
            exp_file_path = exports_dir / f"{export_id}.docx"
            await asyncio.to_thread(build_lesson_plan_docx, task, exp_file_path)
        elif fmt == "html5":
            # §3.6 / §3.7：py_code 为 Python 源码，导出时从 get_slide_markup 还原 HTML
            parts = [
                "<!DOCTYPE html>",
                '<html lang="zh-CN"><head><meta charset="utf-8"/>'
                "<title>PPT Export</title></head><body>",
            ]
            for pid in task.page_order:
                p = task.pages.get(pid)
                if not p:
                    continue
                inner = extract_slide_html_from_py(p.py_code or "")
                parts.append(
                    f'<section class="slide" data-page-id="{pid}">{inner}</section>'
                )
            parts.append("</body></html>")
            exp_file_path = exports_dir / f"{export_id}.html"
            exp_file_path.write_text("\n".join(parts), encoding="utf-8")
        else:
            await task_manager.update_export(
                export_id, status="failed", last_update=utc_ms()
            )
            return

        file_size = exp_file_path.stat().st_size if exp_file_path else 0
        download_url = f"/static/runs/{task_id}/exports/{exp_file_path.name}" if exp_file_path else ""
        await task_manager.update_export(
            export_id,
            status="completed",
            download_url=download_url,
            file_size=file_size,
            last_update=utc_ms(),
            format=fmt,
        )
    except Exception:
        await task_manager.update_export(
            export_id, status="failed", last_update=utc_ms()
        )
        return

    await safe_notify_voice_agent_ppt_status(task_id=task_id, format=fmt)


@app.post("/api/v1/ppt/export")
async def ppt_export(req: PPTExportRequest, request: Request):
    task = await task_manager.get_task(req.task_id)
    if not task:
        return api_error(ec.EC_NOT_FOUND, "task_id 不存在")

    denied = assert_task_access(request, task)
    if denied is not None:
        return denied

    if req.format not in ("pptx", "docx", "html5"):
        return api_error(ec.EC_PARAM, "format 仅支持 pptx/docx/html5")

    export_id = task_manager.new_export_id()
    exp = ExportState(
        export_id=export_id,
        task_id=req.task_id,
        format=req.format,
        status="generating",
        last_update=utc_ms(),
    )
    await task_manager.upsert_export(exp)

    # 后台异步导出
    exp.running_job = asyncio.create_task(
        export_task_background(export_id, req.task_id, req.format)
    )

    return JSONResponse(
        status_code=200,
        content=APIResponse(
            code=200,
            message="success",
            data=PPTExportInitResponseData(
                export_id=export_id, status="generating", estimated_seconds=30
            ).model_dump(),
        ).model_dump(),
    )


@app.get("/api/v1/ppt/export/{export_id}")
async def ppt_export_status(export_id: str, request: Request):
    exp = await task_manager.get_export(export_id)
    if not exp:
        return api_error(ec.EC_NOT_FOUND, "export_id 不存在")
    task = await task_manager.get_task(exp.task_id)
    if not task:
        return api_error(ec.EC_NOT_FOUND, "task_id 不存在")
    denied = assert_task_access(request, task)
    if denied is not None:
        return denied

    data = {
        "export_id": exp.export_id,
        "status": exp.status,
        "download_url": public_media_url(exp.download_url),
        "format": exp.format,
        "file_size": exp.file_size,
    }
    return JSONResponse(
        status_code=200,
        content=APIResponse(code=200, message="success", data=data).model_dump(),
    )


@app.get("/api/v1/ppt/page/{page_id}/render")
async def ppt_page_render(task_id: str, page_id: str, request: Request):
    task = await task_manager.get_task(task_id)
    if not task:
        return api_error(ec.EC_NOT_FOUND, "task_id 不存在")

    denied = assert_task_access(request, task)
    if denied is not None:
        return denied

    p = task.pages.get(page_id)
    if not p:
        return api_error(ec.EC_NOT_FOUND, "page_id 不存在")

    resp = {
        "page_id": p.page_id,
        "task_id": task.task_id,
        "status": p.status,
        "render_url": public_media_url(p.render_url),
        "py_code": p.py_code,
        "version": p.version,
        "updated_at": p.updated_at,
    }
    return JSONResponse(
        status_code=200,
        content=APIResponse(code=200, message="success", data=resp).model_dump(),
    )


@app.post("/api/v1/ppt/canvas/render/execute")
async def canvas_render_execute(body: CanvasRenderExecuteRequest, request: Request):
    """§3.8.3：按任务页执行渲染（写 renders/slide_XXXX.jpg）。"""
    tid = (body.task_id or "").strip()
    if not tid:
        return api_error(ec.EC_PARAM, "task_id 不能为空")
    task = await task_manager.get_task(tid)
    if not task:
        return api_error(ec.EC_NOT_FOUND, "task_id 不存在")

    denied = assert_task_access(request, task)
    if denied is not None:
        return denied

    pid = (body.page_id or "").strip()
    if not pid or pid not in task.pages:
        return api_error(ec.EC_NOT_FOUND, "page_id 不存在")
    p = task.pages[pid]
    override = (body.py_code or "").strip()
    if override:
        p.py_code = override
        p.version += 1
        p.updated_at = utc_ms()
    code = p.py_code
    task_dir = task_manager.task_dir(task.task_id)

    ok, err_msg = await asyncio.to_thread(
        generator.execute_render_job,
        task_dir,
        p.slide_index,
        code,
    )
    ru = render_url_for_task(task.task_id, p.slide_index)
    if not ok:
        em = err_msg or "渲染失败"
        if "未安装" in em or "html2image" in em or "PIL" in em or "deps" in em.lower():
            return api_error(ec.EC_LLM_DEPENDENCY, f"渲染依赖不可用：{em}")
        return api_error(ec.EC_INTERNAL, em)

    p.render_url = ru
    task.last_update = utc_ms()
    await canvas_store.save_canvas_from_task(task)

    data = CanvasRenderExecuteResponseData(
        success=True,
        error="",
        render_url=public_media_url(ru),
    )
    return JSONResponse(
        status_code=200,
        content=APIResponse(
            code=200, message="success", data=data.model_dump()
        ).model_dump(),
    )


@app.get("/healthz")
async def healthz():
    redis_ok = await canvas_store.ping()
    return {"ok": True, "redis": redis_ok, "ppt_agent_port": 9100}


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(
        "ppt_agent_service.app:app",
        host=os.getenv("PPT_AGENT_HOST", "0.0.0.0"),
        port=int(os.getenv("PPT_AGENT_PORT", "9100")),
        reload=os.getenv("PPT_AGENT_RELOAD", "").lower() in ("1", "true", "yes"),
    )

