"""
API 层单元测试（Flask test client，无需真实解析器）
"""
import io
import pytest
import sys, os
sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from api.server import app


@pytest.fixture
def client():
    app.config["TESTING"] = True
    with app.test_client() as c:
        yield c


class TestHealth:
    def test_health_ok(self, client):
        resp = client.get("/health")
        assert resp.status_code == 200
        assert resp.json["status"] == "ok"


class TestParseValidation:
    def test_missing_file_url(self, client):
        resp = client.post("/parse", json={"file_type": "pdf", "doc_id": "doc_1"})
        assert resp.status_code == 400
        assert "file_url" in resp.json["error"]

    def test_missing_file_type(self, client):
        resp = client.post("/parse", json={"file_url": "http://x.com/f.pdf", "doc_id": "doc_1"})
        assert resp.status_code == 400
        assert "file_type" in resp.json["error"]

    def test_missing_doc_id(self, client):
        resp = client.post("/parse", json={"file_url": "http://x.com/f.pdf", "file_type": "pdf"})
        assert resp.status_code == 400
        assert "doc_id" in resp.json["error"]

    def test_unsupported_file_type(self, client):
        resp = client.post("/parse", json={
            "file_url": "http://x.com/f.xyz",
            "file_type": "xyz",
            "doc_id": "doc_1",
        })
        assert resp.status_code == 400
        assert "unsupported" in resp.json["error"]

    def test_invalid_json(self, client):
        resp = client.post("/parse",
                           data=b"not json",
                           content_type="application/json")
        assert resp.status_code == 400


class TestParseSuccessMock:
    """mock download_file，只测 API 层路由和响应结构"""
    def test_response_fields(self, client, monkeypatch):
        import utils.common as common

        def fake_download(url, dest=None):
            # 返回一个空的临时文件用于测试
            path = os.path.join(os.path.dirname(__file__), "..", "tmp", "test_fake.txt")
            os.makedirs(os.path.dirname(path), exist_ok=True)
            with open(path, "w") as f:
                f.write("测试内容。" * 100)
            return path

        monkeypatch.setattr(common, "download_file", fake_download)

        resp = client.post("/parse", json={
            "file_url": "http://example.com/test.pdf",
            "file_type": "pdf",
            "doc_id": "doc_test_001",
        })
        assert resp.status_code == 200
        data = resp.json
        assert "doc_id" in data
        assert "file_type" in data
        assert "title" in data
        assert "text_chunks" in data
        assert "summary" in data
        assert isinstance(data["text_chunks"], list)
