"""DB 层与 TaskManager 共用的轻量 DTO（避免 task_manager ↔ repository 循环导入）。"""
from __future__ import annotations

from dataclasses import dataclass


@dataclass
class SessionSummary:
    session_id: str
    user_id: str
    title: str
    status: str
    created_at: int
    updated_at: int


@dataclass
class TaskListRow:
    task_id: str
    session_id: str
    user_id: str
    topic: str
    status: str
    updated_at: int
