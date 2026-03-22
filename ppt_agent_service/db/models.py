"""
与《系统接口规范》§7.2.1–§7.2.3 对齐的 ORM 模型；tasks 增加 agent_payload 存 PPT 运行时状态 JSON。
"""
from __future__ import annotations

from sqlalchemy import BigInteger, ForeignKey, Index, String, Text
from sqlalchemy.orm import DeclarativeBase, Mapped, mapped_column


class Base(DeclarativeBase):
    pass


class UserRow(Base):
    __tablename__ = "users"

    id: Mapped[str] = mapped_column(String(64), primary_key=True)
    username: Mapped[str] = mapped_column(String(64), nullable=False, unique=True)
    email: Mapped[str] = mapped_column(String(128), nullable=False, unique=True)
    password_hash: Mapped[str] = mapped_column(String(256), nullable=False)
    display_name: Mapped[str] = mapped_column(String(128), default="")
    subject: Mapped[str] = mapped_column(String(64), default="")
    school: Mapped[str] = mapped_column(String(128), default="")
    role: Mapped[str] = mapped_column(String(16), default="teacher")
    created_at: Mapped[int] = mapped_column(BigInteger, nullable=False)
    updated_at: Mapped[int] = mapped_column(BigInteger, nullable=False)


class SessionRow(Base):
    __tablename__ = "sessions"

    id: Mapped[str] = mapped_column(String(64), primary_key=True)
    user_id: Mapped[str] = mapped_column(
        String(64), ForeignKey("users.id", ondelete="CASCADE"), nullable=False
    )
    title: Mapped[str] = mapped_column(String(256), default="")
    status: Mapped[str] = mapped_column(String(16), default="active")
    created_at: Mapped[int] = mapped_column(BigInteger, nullable=False)
    updated_at: Mapped[int] = mapped_column(BigInteger, nullable=False)

    __table_args__ = (Index("idx_sessions_user", "user_id"),)


class TaskRow(Base):
    __tablename__ = "tasks"

    id: Mapped[str] = mapped_column(String(64), primary_key=True)
    session_id: Mapped[str] = mapped_column(
        String(64), ForeignKey("sessions.id", ondelete="CASCADE"), nullable=False
    )
    user_id: Mapped[str] = mapped_column(
        String(64), ForeignKey("users.id", ondelete="CASCADE"), nullable=False
    )
    topic: Mapped[str] = mapped_column(String(256), nullable=False)
    description: Mapped[str] = mapped_column(Text, default="")
    total_pages: Mapped[int] = mapped_column(default=0)
    audience: Mapped[str] = mapped_column(String(128), default="")
    global_style: Mapped[str] = mapped_column(String(128), default="")
    status: Mapped[str] = mapped_column(String(16), default="pending")
    created_at: Mapped[int] = mapped_column(BigInteger, nullable=False)
    updated_at: Mapped[int] = mapped_column(BigInteger, nullable=False)
    #: 页数据、悬挂、合并队列、reference_files、teaching_elements、version、路径等
    agent_payload: Mapped[str] = mapped_column(Text, default="{}")

    __table_args__ = (
        Index("idx_tasks_session", "session_id"),
        Index("idx_tasks_user", "user_id"),
    )


class ExportRow(Base):
    """导出任务（规范 §3.6 扩展）。"""

    __tablename__ = "ppt_exports"

    id: Mapped[str] = mapped_column(String(64), primary_key=True)
    task_id: Mapped[str] = mapped_column(String(64), nullable=False, index=True)
    #: 避免与 PG 保留字 format 冲突
    export_format: Mapped[str] = mapped_column("export_format", String(16), nullable=False)
    status: Mapped[str] = mapped_column(String(16), default="pending")
    download_url: Mapped[str] = mapped_column(String(1024), default="")
    file_size: Mapped[int] = mapped_column(BigInteger, default=0)
    last_update: Mapped[int] = mapped_column(BigInteger, nullable=False)
