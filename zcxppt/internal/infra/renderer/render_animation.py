#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Animation HTML5/GIF/MP4 Renderer

Receives JSON via stdin:
{
    "output_path": str,           # Output path (.html / .gif / .mp4)
    "format": str,                # "html5" | "gif" | "mp4"
    "title": str,                 # Animation title
    "animation_type": str,         # "reveal" | "transition" | "diagram" | "timeline"
    "data": {
        "scenes": [...],          # List of scenes for animation
        "css_keyframes": str,     # Optional CSS keyframe definitions
        "html_content": str       # Optional inline HTML body
    }
}

Scene format:
{
    "id": int,
    "elements": [
        {
            "type": "text"|"shape"|"image"|"chart",
            "content": str,
            "style": {"x": int, "y": int, "w": int, "h": int, "font_size": int, "color": str, ...}
        }
    ],
    "animation": {"type": "fade"|"slide"|"zoom"|"draw"|"typewriter", "duration": float, "delay": float}
}

For GIF format:
    Uses playwright to open HTML, wait for animation, and capture frames
For MP4 format:
    Uses playwright to record video of the animation

Outputs JSON to stdout:
{
    "success": bool,
    "output_path": str,
    "error": str
}
"""

import json
import sys
import traceback
import os


def run():
    raw = sys.stdin.read()
    if not raw.strip():
        print(json.dumps({"success": False, "output_path": "", "error": "empty input"}))
        sys.exit(1)

    try:
        req = json.loads(raw)
    except json.JSONDecodeError as e:
        print(json.dumps({"success": False, "output_path": "", "error": f"invalid json: {e}"}))
        sys.exit(1)

    output_path = req.get("output_path", "animation.html")
    fmt = req.get("format", "html5").lower().strip()
    title = req.get("title", "知识点动画")
    anim_type = req.get("animation_type", "reveal")
    data = req.get("data", {})

    # Build HTML first (always needed)
    html = build_animation_html(title, anim_type, data)

    if fmt == "html5":
        success = write_output(output_path, html)
        print(json.dumps({"success": success, "output_path": output_path,
                          "error": "" if success else "failed to write file"}))
    elif fmt == "gif":
        render_gif(output_path, html, anim_type, data)
    elif fmt == "mp4":
        render_mp4(output_path, html, anim_type, data)
    else:
        # Default to HTML
        success = write_output(output_path, html)
        print(json.dumps({"success": success, "output_path": output_path,
                          "error": "" if success else "failed to write file"}))


def write_output(path, content):
    try:
        os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
        with open(path, "w", encoding="utf-8") as f:
            f.write(content)
        return True
    except Exception as e:
        print(json.dumps({"success": False, "output_path": "", "error": str(e)}))
        sys.exit(1)


# ─────────────────────────────────────────────────────────────────────────────
# Animation HTML builders
# ─────────────────────────────────────────────────────────────────────────────

def build_animation_html(title, anim_type, data):
    scenes = data.get("scenes", [])
    html_content = data.get("html_content", "")
    css_keyframes = data.get("css_keyframes", "")

    if html_content:
        # Custom HTML content — wrap in our template
        body = html_content
    elif scenes:
        body = build_scene_based_animation(scenes, anim_type)
    else:
        body = f"<div class='anim-container'><h2>{title}</h2><p>（无动画内容）</p></div>"

    return f"""<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>{title}</title>
<style>
  * {{ box-sizing: border-box; margin: 0; padding: 0; }}
  body {{
    font-family: "Microsoft YaHei", "PingFang SC", Arial, sans-serif;
    background: #0d1117;
    min-height: 100vh;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    overflow: hidden;
  }}
  .anim-wrapper {{
    width: 100%;
    max-width: 900px;
    aspect-ratio: 16/9;
    position: relative;
    background: #161b22;
    border-radius: 12px;
    overflow: hidden;
    box-shadow: 0 16px 48px rgba(0,0,0,0.6);
  }}
  .anim-container {{
    width: 100%;
    height: 100%;
    position: relative;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 40px;
  }}
  /* Reveal animation types */
  .reveal-text {{
    font-size: 28px;
    color: #58a6ff;
    font-weight: bold;
    opacity: 0;
    animation: fadeReveal 0.8s ease forwards;
  }}
  @keyframes fadeReveal {{
    from {{ opacity: 0; transform: translateY(20px); }}
    to {{ opacity: 1; transform: translateY(0); }}
  }}
  /* Slide animation */
  .slide-element {{
    opacity: 0;
    animation: slideIn 0.6s ease forwards;
  }}
  @keyframes slideIn {{
    from {{ opacity: 0; transform: translateX(-60px); }}
    to {{ opacity: 1; transform: translateX(0); }}
  }}
  /* Zoom animation */
  .zoom-element {{
    opacity: 0;
    transform: scale(0.5);
    animation: zoomIn 0.5s ease forwards;
  }}
  @keyframes zoomIn {{
    to {{ opacity: 1; transform: scale(1); }}
  }}
  /* Typewriter */
  .typewriter {{
    overflow: hidden;
    border-right: 3px solid #58a6ff;
    white-space: nowrap;
    animation: typing 2s steps(30) forwards, blink 0.7s step-end infinite;
    font-family: "Courier New", monospace;
    font-size: 22px;
    color: #7ee787;
  }}
  @keyframes typing {{
    from {{ width: 0; }}
    to {{ width: 100%; }}
  }}
  @keyframes blink {{
    50% {{ border-color: transparent; }}
  }}
  /* Scene transition */
  .scene-slide {{
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 40px;
    opacity: 0;
    transition: opacity 0.6s ease;
  }}
  .scene-slide.active {{ opacity: 1; }}
  /* Diagram */
  .diagram-box {{
    background: #21262d;
    border: 2px solid #30363d;
    border-radius: 8px;
    padding: 16px 24px;
    color: #c9d1d9;
    font-size: 16px;
    margin: 8px;
    opacity: 0;
    animation: fadeReveal 0.6s ease forwards;
  }}
  .diagram-arrow {{
    color: #58a6ff;
    font-size: 24px;
    opacity: 0;
    animation: fadeReveal 0.4s ease 0.3s forwards;
  }}
  /* Timeline */
  .timeline {{
    position: relative;
    width: 100%;
    padding: 20px 40px;
  }}
  .timeline::before {{
    content: '';
    position: absolute;
    left: 50%;
    top: 0;
    bottom: 0;
    width: 3px;
    background: #30363d;
    transform: translateX(-50%);
  }}
  .timeline-item {{
    display: flex;
    align-items: center;
    margin: 20px 0;
    opacity: 0;
  }}
  .timeline-item:nth-child(odd) {{ flex-direction: row; justify-content: flex-start; }}
  .timeline-item:nth-child(even) {{ flex-direction: row-reverse; justify-content: flex-start; }}
  .timeline-dot {{
    width: 16px;
    height: 16px;
    border-radius: 50%;
    background: #58a6ff;
    border: 3px solid #0d1117;
    position: absolute;
    left: 50%;
    transform: translateX(-50%);
    z-index: 1;
  }}
  .timeline-content {{
    width: 45%;
    background: #21262d;
    border-radius: 8px;
    padding: 12px 16px;
    color: #c9d1d9;
    font-size: 14px;
  }}
  /* Progress indicator */
  .scene-dots {{
    position: absolute;
    bottom: 16px;
    left: 50%;
    transform: translateX(-50%);
    display: flex;
    gap: 8px;
  }}
  .dot {{
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: #30363d;
    cursor: pointer;
    transition: background 0.3s;
  }}
  .dot.active {{ background: #58a6ff; }}
  /* Auto-play controls */
  .controls {{
    margin-top: 16px;
    display: flex;
    gap: 12px;
    align-items: center;
  }}
  .ctrl-btn {{
    background: #21262d;
    color: #c9d1d9;
    border: 1px solid #30363d;
    border-radius: 6px;
    padding: 8px 20px;
    font-size: 14px;
    cursor: pointer;
    font-family: inherit;
    transition: all 0.2s;
  }}
  .ctrl-btn:hover {{ background: #30363d; }}
  .progress-bar {{
    width: 200px;
    height: 4px;
    background: #30363d;
    border-radius: 2px;
    overflow: hidden;
  }}
  .progress-fill {{
    height: 100%;
    background: #58a6ff;
    transition: width 0.1s linear;
  }}
{css_keyframes}
</style>
</head>
<body>
<div class="anim-wrapper">
  {body}
</div>
<div class="controls">
  <button class="ctrl-btn" id="prevBtn" onclick="prevScene()">&#8592; 上一页</button>
  <div class="progress-bar"><div class="progress-fill" id="progFill"></div></div>
  <button class="ctrl-btn" id="nextBtn" onclick="nextScene()">下一页 &#8594;</button>
</div>
<script>
const SCENE_COUNT = {len(scenes)};
let curScene = 0;
let timer = null;

function showScene(idx) {{
  document.querySelectorAll('.scene-slide').forEach(function(el) {{
    el.classList.remove('active');
  }});
  var scene = document.getElementById('scene_'+idx);
  if (scene) scene.classList.add('active');
  document.querySelectorAll('.dot').forEach(function(d, i) {{
    d.classList.toggle('active', i === idx);
  }});
  document.getElementById('progFill').style.width = ((idx + 1) / SCENE_COUNT * 100) + '%';
}}

function nextScene() {{
  curScene = (curScene + 1) % SCENE_COUNT;
  showScene(curScene);
}}

function prevScene() {{
  curScene = (curScene - 1 + SCENE_COUNT) % SCENE_COUNT;
  showScene(curScene);
}}

function autoPlay() {{
  timer = setInterval(nextScene, 3000);
}}

document.addEventListener('DOMContentLoaded', function() {{
  showScene(0);
  autoPlay();
}});
</script>
</body>
</html>"""


def build_scene_based_animation(scenes, anim_type):
    """Build scene-based animation with CSS-driven transitions."""
    scene_divs = ""
    dots = ""
    for i, scene in enumerate(scenes):
        elements_html = ""
        for el in scene.get("elements", []):
            etype = el.get("type", "text")
            content = el.get("content", "")
            style = el.get("style", {})

            inline_style = build_inline_style(style)
            delay = scene.get("animation", {}).get("delay", i * 0.5)
            anim_class = get_animation_class(anim_type)

            if etype == "text":
                elements_html += f'<div class="{anim_class}" style="{inline_style}; animation-delay:{delay}s">{content}</div>'
            elif etype == "shape":
                shape_type = style.get("shape", "rect")
                bg = style.get("background", "#58a6ff")
                elements_html += f'<div class="{anim_class}" style="{inline_style}; background:{bg}; animation-delay:{delay}s"></div>'
            elif etype == "image":
                elements_html += f'<img class="{anim_class}" src="{content}" style="{inline_style}; animation-delay:{delay}s" alt="scene">'
            else:
                elements_html += f'<div class="{anim_class}" style="{inline_style}; animation-delay:{delay}s">{content}</div>'

        scene_divs += (
            f'<div class="scene-slide" id="scene_{i}" style="{"display:none" if i>0 else ""}">'
            f'{elements_html}</div>'
        )
        dots += f'<div class="dot{" active" if i==0 else ""}" onclick="showScene({i})"></div>'

    return f"""
  {scene_divs}
  <div class="scene-dots">{dots}</div>"""


def build_inline_style(style):
    """Convert style dict to inline CSS string."""
    parts = []
    mapping = {
        "x": "left", "y": "top", "w": "width", "h": "height",
        "font_size": "font-size", "color": "color", "background": "background",
        "font_weight": "font-weight", "text_align": "text-align",
        "padding": "padding", "border_radius": "border-radius",
        "border": "border", "margin": "margin",
        "flex": "flex", "gap": "gap", "position": "position",
        "transform": "transform", "opacity": "opacity",
    }
    for k, v in style.items():
        css_prop = mapping.get(k, k)
        if isinstance(v, (int, float)) and css_prop not in ("opacity",):
            v = f"{v}px"
        parts.append(f"{css_prop}:{v}")
    return "; ".join(parts) if parts else ""


def get_animation_class(anim_type):
    mapping = {
        "reveal": "reveal-text",
        "slide": "slide-element",
        "zoom": "zoom-element",
        "typewriter": "typewriter",
        "diagram": "diagram-box",
        "timeline": "timeline-item",
    }
    return mapping.get(anim_type, "reveal-text")


# ─────────────────────────────────────────────────────────────────────────────
# GIF rendering via playwright
# ─────────────────────────────────────────────────────────────────────────────

def render_gif(output_path, html, anim_type, data):
    """Render animation to GIF using playwright."""
    try:
        from playwright.sync_api import sync_playwright
    except ImportError:
        print(json.dumps({
            "success": False, "output_path": "",
            "error": "playwright not installed: pip install playwright && playwright install chromium"
        }))
        sys.exit(1)

    scenes = data.get("scenes", [])
    scene_count = len(scenes) if scenes else 3
    fps = 2  # frames per scene — slow enough to see each scene
    duration_per_scene = 1.5  # seconds

    tmp_html = output_path.replace(".gif", "_anim.html")
    tmp_png = output_path.replace(".gif", "_frame.png")

    try:
        # Write temp HTML
        with open(tmp_html, "w", encoding="utf-8") as f:
            f.write(html)

        from PIL import Image, ImageDraw
        frames = []

        with sync_playwright() as p:
            browser = p.chromium.launch()
            page = browser.new_page(viewport={"width": 900, "height": 506})

            for i in range(scene_count):
                # Navigate to force re-render of scene
                page.goto(f"file://{os.path.abspath(tmp_html)}#scene_{i}")
                page.evaluate(f"showScene({i})")
                page.wait_for_timeout(int(duration_per_scene * 1000))

                # Capture frame
                page.screenshot(path=tmp_png, type="png")
                try:
                    frame = Image.open(tmp_png).copy()
                    frames.append(frame)
                except Exception:
                    pass

                # Duplicate frame for GIF duration
                for _ in range(fps - 1):
                    if frames:
                        frames.append(frames[-1].copy())

            browser.close()

        if len(frames) < 2:
            print(json.dumps({"success": False, "output_path": "",
                              "error": "failed to capture enough frames"}))
            sys.exit(1)

        # Optimize and save GIF
        frames[0].save(
            output_path,
            save_all=True,
            append_images=frames[1:],
            duration=int(duration_per_scene * 1000 / fps),
            loop=0,
            optimize=True,
        )

        # Cleanup
        for f in [tmp_html, tmp_png]:
            if os.path.exists(f):
                os.remove(f)

        print(json.dumps({"success": True, "output_path": output_path, "error": ""}))

    except Exception as e:
        tb = traceback.format_exc()
        print(json.dumps({"success": False, "output_path": "",
                          "error": f"{type(e).__name__}: {e}\n{tb}"}))


# ─────────────────────────────────────────────────────────────────────────────
# MP4 rendering via playwright
# ─────────────────────────────────────────────────────────────────────────────

def render_mp4(output_path, html, anim_type, data):
    """Render animation to MP4 using playwright video recording."""
    try:
        from playwright.sync_api import sync_playwright
    except ImportError:
        print(json.dumps({
            "success": False, "output_path": "",
            "error": "playwright not installed: pip install playwright && playwright install chromium"
        }))
        sys.exit(1)

    scenes = data.get("scenes", [])
    scene_count = len(scenes) if scenes else 3
    duration_per_scene = 2.0  # seconds

    tmp_html = output_path.replace(".mp4", "_anim.html")
    tmp_video = output_path.replace(".mp4", "_tmp.webm")

    try:
        with open(tmp_html, "w", encoding="utf-8") as f:
            f.write(html)

        with sync_playwright() as p:
            browser = p.chromium.launch()
            context = browser.contexts[0] if browser.contexts else browser.new_context()
            page = context.new_page(viewport={"width": 900, "height": 506})

            # Start video recording
            page.video.start_recording(path=tmp_video)

            # Cycle through scenes
            page.goto(f"file://{os.path.abspath(tmp_html)}")
            page.wait_for_timeout(500)
            showScene_js = "showScene" in page.content()

            for i in range(scene_count):
                page.evaluate(f"showScene({i})")
                page.wait_for_timeout(int(duration_per_scene * 1000))

            # Stop and save video
            video = page.video.stop_recording()
            path = page.video.save_as(path=tmp_video)

            browser.close()

        # Convert webm to mp4 via ffmpeg if available, else just rename
        try:
            import subprocess
            subprocess.run(
                ["ffmpeg", "-y", "-i", tmp_video, "-c:v", "libx264", "-preset", "fast", output_path],
                capture_output=True, timeout=60,
            )
            converted = os.path.exists(output_path)
        except Exception:
            converted = False

        if not converted:
            # Fallback: copy as mp4 (will be webm but with .mp4 extension)
            import shutil
            shutil.copy(tmp_video, output_path)

        for f in [tmp_html, tmp_video]:
            if os.path.exists(f):
                os.remove(f)

        print(json.dumps({"success": True, "output_path": output_path, "error": ""}))

    except Exception as e:
        tb = traceback.format_exc()
        print(json.dumps({"success": False, "output_path": "",
                          "error": f"{type(e).__name__}: {e}\n{tb}"}))


if __name__ == "__main__":
    try:
        run()
    except Exception as e:
        tb = traceback.format_exc()
        print(json.dumps({
            "success": False,
            "output_path": "",
            "error": f"{type(e).__name__}: {e}\n{tb}"
        }))
        sys.exit(1)
