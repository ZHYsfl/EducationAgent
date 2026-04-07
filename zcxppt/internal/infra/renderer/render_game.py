#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Interactive Game HTML5 Renderer

Receives JSON via stdin:
{
    "output_path": str,      # Output HTML path
    "game_type": str,        # "quiz" | "matching" | "ordering" | "fill_blank"
    "title": str,           # Game title
    "data": list            # Game data (questions, answers, etc.)
}

For quiz:
    data = [{"question": str, "options": [str,str,str,str], "answer": int, "explanation": str}]
For matching:
    data = [{"left": str, "right": str}]
For fill_blank:
    data = [{"sentence": str, "answer": str, "clue": str}]
For ordering:
    data = [{"items": [str], "correct_order": [int]}]

Outputs JSON to stdout:
{
    "success": bool,
    "html_path": str,
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
        print(json.dumps({"success": False, "html_path": "", "error": "empty input"}))
        sys.exit(1)

    try:
        req = json.loads(raw)
    except json.JSONDecodeError as e:
        print(json.dumps({"success": False, "html_path": "", "error": f"invalid json: {e}"}))
        sys.exit(1)

    output_path = req.get("output_path", "game.html")
    game_type = req.get("game_type", "quiz")
    title = req.get("title", "互动小游戏")
    data = req.get("data", [])

    if not data:
        print(json.dumps({"success": False, "html_path": "", "error": "data field is required and must not be empty"}))
        sys.exit(1)

    game_type_lower = game_type.lower().strip()
    if game_type_lower == "quiz":
        html = build_quiz_game(title, data)
    elif game_type_lower == "matching":
        html = build_matching_game(title, data)
    elif game_type_lower == "ordering":
        html = build_ordering_game(title, data)
    elif game_type_lower == "fill_blank":
        html = build_fill_blank_game(title, data)
    else:
        # Default to quiz
        html = build_quiz_game(title, data)

    try:
        os.makedirs(os.path.dirname(output_path) or ".", exist_ok=True)
        with open(output_path, "w", encoding="utf-8") as f:
            f.write(html)
        print(json.dumps({"success": True, "html_path": output_path, "error": ""}))
    except Exception as e:
        print(json.dumps({"success": False, "html_path": "", "error": str(e)}))


# ─────────────────────────────────────────────────────────────────────────────
# Shared CSS / JS header
# ─────────────────────────────────────────────────────────────────────────────

SHARED_HEADER = """
<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    font-family: "Microsoft YaHei", "PingFang SC", Arial, sans-serif;
    background: #1a1a2e;
    color: #eee;
    min-height: 100vh;
    display: flex;
    flex-direction: column;
    align-items: center;
    padding: 20px;
  }
  .game-container {
    width: 100%;
    max-width: 720px;
    background: #16213e;
    border-radius: 16px;
    padding: 30px;
    box-shadow: 0 8px 32px rgba(0,0,0,0.4);
    animation: fadeIn 0.5s ease;
  }
  @keyframes fadeIn { from { opacity: 0; transform: translateY(20px); } to { opacity: 1; } }
  .game-header {
    text-align: center;
    border-bottom: 2px solid #1F4E79;
    padding-bottom: 15px;
    margin-bottom: 25px;
  }
  .game-title { font-size: 24px; font-weight: bold; color: #4fc3f7; }
  .progress-bar {
    height: 6px;
    background: #2a3a5a;
    border-radius: 3px;
    margin-bottom: 20px;
    overflow: hidden;
  }
  .progress-fill {
    height: 100%;
    background: linear-gradient(90deg, #4fc3f7, #1F4E79);
    border-radius: 3px;
    transition: width 0.4s ease;
  }
  .card { animation: slideIn 0.3s ease; }
  @keyframes slideIn { from { opacity: 0; transform: translateX(30px); } to { opacity: 1; } }
  .question { font-size: 18px; font-weight: bold; color: #fff; margin-bottom: 20px; line-height: 1.6; }
  .options { display: flex; flex-direction: column; gap: 12px; }
  .option-btn {
    background: #fff;
    color: #222;
    border: 2px solid #e0e0e0;
    border-radius: 10px;
    padding: 14px 20px;
    font-size: 15px;
    font-family: inherit;
    cursor: pointer;
    text-align: left;
    transition: all 0.2s ease;
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .option-btn:hover:not(:disabled) {
    background: #e3f2fd;
    border-color: #4472C4;
    transform: translateX(4px);
  }
  .option-btn.selected-correct {
    background: #c8e6c9 !important;
    border-color: #4caf50 !important;
    color: #1b5e20;
  }
  .option-btn.selected-wrong {
    background: #ffcdd2 !important;
    border-color: #f44336 !important;
    color: #b71c1c;
  }
  .option-btn .opt-label {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    border-radius: 50%;
    background: #4472C4;
    color: #fff;
    font-weight: bold;
    font-size: 13px;
    flex-shrink: 0;
  }
  .explanation-box {
    margin-top: 16px;
    padding: 14px;
    border-radius: 8px;
    font-size: 14px;
    line-height: 1.6;
    display: none;
  }
  .explanation-correct { background: #e8f5e9; color: #1b5e20; border-left: 4px solid #4caf50; }
  .explanation-wrong { background: #fce4ec; color: #b71c1c; border-left: 4px solid #f44336; }
  .next-btn {
    display: none;
    margin: 20px auto 0;
    background: #1F4E79;
    color: #fff;
    border: none;
    border-radius: 25px;
    padding: 12px 40px;
    font-size: 16px;
    font-family: inherit;
    cursor: pointer;
    transition: background 0.2s;
  }
  .next-btn:hover { background: #2a6fb0; }
  /* Result screen */
  .result-card {
    text-align: center;
    padding: 20px 0;
  }
  .score-circle {
    width: 140px;
    height: 140px;
    border-radius: 50%;
    margin: 0 auto 20px;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-direction: column;
    font-size: 36px;
    font-weight: bold;
    color: #fff;
  }
  .score-full { background: linear-gradient(135deg, #4caf50, #81c784); }
  .score-mid { background: linear-gradient(135deg, #ff9800, #ffb74d); }
  .score-low { background: linear-gradient(135deg, #f44336, #e57373); }
  .score-label { font-size: 14px; font-weight: normal; margin-top: 4px; }
  .result-text { font-size: 20px; margin-bottom: 25px; color: #ccc; }
  .result-detail { text-align: left; background: #1a1a2e; border-radius: 10px; padding: 15px; font-size: 13px; }
  .result-item { padding: 6px 0; border-bottom: 1px solid #2a3a5a; }
  .result-item:last-child { border-bottom: none; }
  .result-correct { color: #4caf50; }
  .result-wrong { color: #f44336; }
  .play-again-btn {
    margin-top: 25px;
    background: #4fc3f7;
    color: #1a1a2e;
    border: none;
    border-radius: 25px;
    padding: 12px 40px;
    font-size: 16px;
    font-weight: bold;
    font-family: inherit;
    cursor: pointer;
    transition: all 0.2s;
  }
  .play-again-btn:hover { background: #81d4fa; transform: scale(1.05); }
  /* Matching game */
  .matching-container { display: flex; gap: 20px; justify-content: space-between; }
  .matching-column { flex: 1; display: flex; flex-direction: column; gap: 10px; }
  .matching-item {
    padding: 12px 16px;
    border-radius: 8px;
    font-size: 14px;
    cursor: pointer;
    transition: all 0.2s;
    border: 2px solid transparent;
    text-align: center;
  }
  .matching-left { background: #e3f2fd; color: #1565c0; font-weight: bold; }
  .matching-right { background: #f3e5f5; color: #6a1b9a; }
  .matching-item:hover { transform: scale(1.03); box-shadow: 0 2px 8px rgba(0,0,0,0.2); }
  .matching-item.selected { border-color: #4fc3f7; transform: scale(1.05); box-shadow: 0 0 0 3px rgba(79,195,247,0.4); }
  .matching-item.matched { opacity: 0.4; pointer-events: none; background: #c8e6c9 !important; border-color: #4caf50; }
  .matching-item.wrong-match { animation: shake 0.4s ease; }
  @keyframes shake { 0%,100%{transform:translateX(0)} 25%{transform:translateX(-5px)} 75%{transform:translateX(5px)} }
  .matching-status { text-align: center; margin: 15px 0; font-size: 16px; color: #4fc3f7; }
  /* Ordering game */
  .ordering-pool { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 20px; justify-content: center; }
  .ordering-item {
    padding: 10px 16px;
    background: #e3f2fd;
    color: #1565c0;
    border-radius: 20px;
    cursor: pointer;
    font-size: 14px;
    transition: all 0.2s;
    border: 2px solid transparent;
  }
  .ordering-item:hover { background: #bbdefb; transform: scale(1.05); }
  .ordering-item.used { opacity: 0.3; pointer-events: none; }
  .ordering-answer { display: flex; flex-wrap: wrap; gap: 8px; justify-content: center; min-height: 50px; margin-bottom: 15px; }
  .ordering-slot {
    padding: 10px 16px;
    background: #fff;
    color: #4472C4;
    border-radius: 20px;
    font-size: 14px;
    border: 2px dashed #4472C4;
    cursor: pointer;
    transition: all 0.2s;
    min-width: 80px;
    text-align: center;
  }
  .ordering-slot:hover { background: #e3f2fd; }
  /* Fill blank */
  .sentence-box {
    background: #fff;
    color: #222;
    padding: 20px;
    border-radius: 10px;
    font-size: 16px;
    line-height: 2;
    margin-bottom: 15px;
  }
  .blank-underline { display: inline-block; border-bottom: 2px solid #4472C4; min-width: 80px; text-align: center; color: #4472C4; font-weight: bold; }
  .clue-box { background: #fff3e0; color: #e65100; padding: 10px 15px; border-radius: 8px; font-size: 13px; margin-bottom: 15px; }
  .answer-input { display: flex; gap: 10px; margin-bottom: 15px; }
  .answer-input input {
    flex: 1;
    padding: 12px 16px;
    border: 2px solid #4472C4;
    border-radius: 8px;
    font-size: 16px;
    font-family: inherit;
    outline: none;
    background: #fff;
    color: #222;
  }
  .answer-input input:focus { border-color: #4fc3f7; }
  .submit-btn {
    background: #4472C4;
    color: #fff;
    border: none;
    border-radius: 8px;
    padding: 12px 30px;
    font-size: 16px;
    font-family: inherit;
    cursor: pointer;
    transition: background 0.2s;
  }
  .submit-btn:hover { background: #2851a3; }
  .check-result { margin-top: 10px; padding: 12px; border-radius: 8px; font-size: 14px; display: none; }
</style>
</head>
"""


# ─────────────────────────────────────────────────────────────────────────────
# Quiz Game Builder
# ─────────────────────────────────────────────────────────────────────────────

def build_quiz_game(title, data):
    questions_json = json.dumps(data, ensure_ascii=False)
    options_html = ""
    for i, q in enumerate(data):
        opts = q.get("options", [])
        exp = q.get("explanation", "").replace('"', '\\"').replace("\n", "\\n")
        exp_correct = f'<div class="explanation-box explanation-correct" id="exp{i}"><strong>&#10004; 正确！</strong> {exp}</div>'
        exp_wrong = f'<div class="explanation-box explanation-wrong" id="exp{i}"><strong>&#10006; 错误！</strong> {exp}</div>'

        opts_html = ""
        for j, opt in enumerate(opts):
            display_letter = chr(65 + j)  # A, B, C, D
            is_correct = (j == q.get("answer", 0))
            correct_class = "correct-opt" if is_correct else ""
            opts_html += (
                f'<button class="option-btn {correct_class}" data-correct="{is_correct}" '
                f'data-index="{i}" onclick="selectOption(this, {j}, {is_correct})">'
                f'<span class="opt-label">{display_letter}</span><span>{opt}</span></button>\n'
            )
        options_html += (
            f'<div class="card" id="q{i}" style="{"display:none" if i>0 else ""}">'
            f'<div class="question">{i+1}. {q.get("question","")}</div>'
            f'<div class="options">{opts_html}</div>'
            f'{exp_correct}{exp_wrong}'
            f'<button class="next-btn" id="next{i}" onclick="nextQuestion({i})">下一题 &#8594;</button>'
            f'</div>'
        )

    body_script = f"""
<body>
<div class="game-container">
  <div class="game-header">
    <div class="game-title">{title}</div>
  </div>
  <div class="progress-bar"><div class="progress-fill" id="progressBar" style="width:0%"></div></div>
  <div id="questionsContainer">{options_html}</div>
  <div class="card result-card" id="resultCard" style="display:none">
    <div class="score-circle" id="scoreCircle"><span id="scoreNum">0</span><div class="score-label">分</div></div>
    <div class="result-text" id="resultText"></div>
    <div class="result-detail" id="resultDetail"></div>
    <button class="play-again-btn" onclick="location.reload()">再玩一次</button>
  </div>
</div>
<script>
const QUESTIONS = {questions_json};
let curQ = 0;
let score = 0;
let answered = [];

function showQuestion(idx) {{
  document.querySelectorAll('.card').forEach(c => {{ if (!c.id.includes('result')) c.style.display = 'none'; }});
  var q = document.getElementById('q'+idx);
  if (q) q.style.display = 'block';
  document.getElementById('progressBar').style.width = ((idx) / QUESTIONS.length * 100) + '%';
}}

function selectOption(btn, optIdx, isCorrect) {{
  if (answered[curQ] !== undefined) return;
  answered[curQ] = optIdx;
  var btns = document.querySelectorAll('#q'+curQ+' .option-btn');
  btns.forEach(function(b) {{ b.disabled = true; }});
  var expEl = document.getElementById('exp'+curQ);
  if (isCorrect) {{
    btn.classList.add('selected-correct');
    score++;
    if (expEl) {{ expEl.classList.add('explanation-correct'); expEl.style.display = 'block'; }}
  }} else {{
    btn.classList.add('selected-wrong');
    // Highlight correct
    btns.forEach(function(b) {{ if (b.dataset.correct === 'true') b.classList.add('selected-correct'); }});
    if (expEl) {{ expEl.classList.add('explanation-wrong'); expEl.style.display = 'block'; }}
  }}
  var nextBtn = document.getElementById('next'+curQ);
  if (nextBtn) nextBtn.style.display = 'block';
}}

function nextQuestion(idx) {{
  curQ = idx + 1;
  if (curQ >= QUESTIONS.length) {{
    showResult();
  }} else {{
    showQuestion(curQ);
  }}
}}

function showResult() {{
  document.querySelectorAll('.card').forEach(c => c.style.display = 'none');
  document.getElementById('resultCard').style.display = 'block';
  document.getElementById('progressBar').style.width = '100%';
  var pct = Math.round(score / QUESTIONS.length * 100);
  var circle = document.getElementById('scoreCircle');
  circle.className = 'score-circle ' + (pct >= 80 ? 'score-full' : pct >= 50 ? 'score-mid' : 'score-low');
  document.getElementById('scoreNum').textContent = score;
  var msgs = ['继续加油！', '还不错！', '太棒了！'];
  var msgIdx = pct >= 80 ? 2 : pct >= 50 ? 1 : 0;
  document.getElementById('resultText').textContent = `你答对了 ${{score}} / ${{QUESTIONS.length}} 题，${{msgs[msgIdx]}}`;

  var detail = '';
  for (var i = 0; i < QUESTIONS.length; i++) {{
    var correct = answered[i] === QUESTIONS[i].answer;
    detail += '<div class="result-item"><span class="' + (correct ? 'result-correct' : 'result-wrong') + '">'
           + (correct ? '&#10004;' : '&#10006;') + ' 第' + (i+1) + '题：' + QUESTIONS[i].question + '</span></div>';
  }}
  document.getElementById('resultDetail').innerHTML = detail;
}}
showQuestion(0);
</script>
</body>
</html>"""
    return SHARED_HEADER + body_script


# ─────────────────────────────────────────────────────────────────────────────
# Matching Game Builder
# ─────────────────────────────────────────────────────────────────────────────

def build_matching_game(title, data):
    pairs_json = json.dumps(data, ensure_ascii=False)
    left_html = ""
    right_html = ""
    for i, p in enumerate(data):
        left_html += (
            f'<div class="matching-item matching-left" id="left_{i}" data-idx="{i}" '
            f'onclick="selectLeft({i})">{p.get("left","")}</div>'
        )
        right_html += (
            f'<div class="matching-item matching-right" id="right_{i}" data-idx="{i}" '
            f'onclick="selectRight({i})">{p.get("right","")}</div>'
        )

    body_script = f"""
<body>
<div class="game-container">
  <div class="game-header"><div class="game-title">{title}</div></div>
  <div class="matching-status" id="statusText">请先点击左侧词语，再点击右侧对应解释</div>
  <div class="matching-container">
    <div class="matching-column">{left_html}</div>
    <div class="matching-column">{right_html}</div>
  </div>
  <div class="progress-bar"><div class="progress-fill" id="progressBar" style="width:0%"></div></div>
  <div class="result-detail" id="resultDetail" style="display:none;margin-top:20px"></div>
</div>
<script>
const PAIRS = {pairs_json};
let selectedLeft = null;
let matched = 0;
let attempts = 0;

function selectLeft(idx) {{
  if (document.getElementById('left_'+idx).classList.contains('matched')) return;
  document.querySelectorAll('.matching-item').forEach(function(el) {{ el.classList.remove('selected'); }});
  document.getElementById('left_'+idx).classList.add('selected');
  selectedLeft = idx;
}}

function selectRight(idx) {{
  if (selectedLeft === null) return;
  if (document.getElementById('right_'+idx).classList.contains('matched')) return;
  var leftEl = document.getElementById('left_'+selectedLeft);
  var rightEl = document.getElementById('right_'+idx);
  attempts++;
  if (selectedLeft === idx) {{
    leftEl.classList.remove('selected');
    leftEl.classList.add('matched');
    rightEl.classList.add('matched');
    matched++;
    document.getElementById('progressBar').style.width = (matched / PAIRS.length * 100) + '%';
    document.getElementById('statusText').textContent = `已匹配 ${{matched}} / ${{PAIRS.length}}，继续加油！`;
    if (matched === PAIRS.length) {{
      document.getElementById('statusText').textContent = '全部匹配正确！太棒了！';
      document.getElementById('statusText').style.color = '#4caf50';
    }}
  }} else {{
    leftEl.classList.add('wrong-match');
    rightEl.classList.add('wrong-match');
    setTimeout(function() {{
      leftEl.classList.remove('wrong-match','selected');
      rightEl.classList.remove('wrong-match');
    }}, 400);
    document.getElementById('statusText').textContent = '不匹配，再试试！';
  }}
  selectedLeft = null;
}}
</script>
</body>
</html>"""
    return SHARED_HEADER + body_script


# ─────────────────────────────────────────────────────────────────────────────
# Ordering Game Builder
# ─────────────────────────────────────────────────────────────────────────────

def build_ordering_game(title, data):
    # Only use first item for simplicity
    if not data:
        return SHARED_HEADER + "<body><div>无数据</div></body>"
    item = data[0]
    items = item.get("items", [])
    correct_order = item.get("correct_order", list(range(len(items))))
    correct_json = json.dumps(correct_order, ensure_ascii=False)
    items_json = json.dumps(items, ensure_ascii=False)
    shuffled = items[:]
    import random
    random.seed(42)
    random.shuffle(shuffled)

    pool_html = "".join(
        f'<div class="ordering-item" id="pool_{i}" onclick="addToAnswer({i})">{shuffled[i]}</div>'
        for i in range(len(shuffled))
    )

    body_script = f"""
<body>
<div class="game-container">
  <div class="game-header"><div class="game-title">{title}</div></div>
  <div class="question" style="margin-bottom:15px;text-align:center">请按正确顺序排列下方内容</div>
  <div class="ordering-pool" id="pool">{pool_html}</div>
  <div>点击下方序号框放入答案，再点击已放入的选项可移除：</div>
  <div class="ordering-answer" id="answerArea"></div>
  <button class="submit-btn" onclick="checkOrder()">提交答案</button>
  <div class="check-result" id="checkResult"></div>
</div>
<script>
const ITEMS = {items_json};
const CORRECT = {correct_json};
let answer = [];
let poolItems = {json.dumps(dict(enumerate(shuffled)))};

function addToAnswer(idx) {{
  var el = document.getElementById('pool_'+idx);
  if (!el || el.classList.contains('used')) return;
  el.classList.add('used');
  answer.push(idx);
  var slot = document.createElement('div');
  slot.className = 'ordering-slot';
  slot.id = 'slot_'+answer.length;
  slot.textContent = ITEMS[idx];
  slot.onclick = function() {{ removeFromAnswer(answer.length - 1); }};
  document.getElementById('answerArea').appendChild(slot);
}}

function removeFromAnswer(pos) {{
  var idx = answer[pos];
  answer.splice(pos, 1);
  document.getElementById('answerArea').innerHTML = '';
  for (var i = 0; i < answer.length; i++) {{
    var s = document.createElement('div');
    s.className = 'ordering-slot';
    s.textContent = ITEMS[answer[i]];
    s.onclick = function() {{ removeFromAnswer(i); }};
    document.getElementById('answerArea').appendChild(s);
  }}
  document.getElementById('pool_'+idx).classList.remove('used');
}}

function checkOrder() {{
  if (answer.length < ITEMS.length) {{
    document.getElementById('checkResult').style.display = 'block';
    document.getElementById('checkResult').style.background = '#fff3e0';
    document.getElementById('checkResult').style.color = '#e65100';
    document.getElementById('checkResult').textContent = '请放入所有选项后再提交！';
    return;
  }}
  var correct = answer.every(function(v,i) {{ return v === CORRECT[i]; }});
  var result = document.getElementById('checkResult');
  result.style.display = 'block';
  if (correct) {{
    result.style.background = '#e8f5e9';
    result.style.color = '#1b5e20';
    result.innerHTML = '<strong>&#10004; 完全正确！太棒了！</strong>';
  }} else {{
    result.style.background = '#fce4ec';
    result.style.color = '#b71c1c';
    result.innerHTML = '<strong>&#10006; 顺序不正确，请重新排列。</strong>';
  }}
}}
</script>
</body>
</html>"""
    return SHARED_HEADER + body_script


# ─────────────────────────────────────────────────────────────────────────────
# Fill-in-the-Blank Game Builder
# ─────────────────────────────────────────────────────────────────────────────

def build_fill_blank_game(title, data):
    questions_json = json.dumps(data, ensure_ascii=False)
    cards_html = ""
    for i, q in enumerate(data):
        sentence = q.get("sentence", "")
        # Replace [blank] or ___ with underlined placeholder
        import re
        sentence_html = re.sub(
            r'\[([^\]]+)\]', r'<span class="blank-underline" id="blank_'+str(i)+'">______</span>', sentence
        )
        sentence_html = re.sub(
            r'_{3,}', lambda m: f'<span class="blank-underline" id="blank_{i}">{"_"*len(m.group())}</span>', sentence_html
        )
        clue = q.get("clue", "")
        exp = q.get("explanation", "").replace('"', '\\"').replace("\n", "\\n")
        cards_html += (
            f'<div class="card" id="fb_{i}" style="{"display:none" if i>0 else ""}">'
            f'<div class="sentence-box" id="sentence_{i}">{sentence_html}</div>'
            f'<div class="clue-box">&#128161; 提示：{clue}</div>'
            f'<div class="answer-input">'
            f'<input type="text" id="answer_{i}" placeholder="请输入答案" onkeydown="if(event.key===\'Enter\')checkFB({i})">'
            f'<button class="submit-btn" onclick="checkFB({i})">提交</button>'
            f'</div>'
            f'<div class="check-result" id="fbResult_{i}"></div>'
            f'<button class="next-btn" id="fbNext_{i}" onclick="nextFB({i})">下一题 &#8594;</button>'
            f'</div>'
        )

    body_script = f"""
<body>
<div class="game-container">
  <div class="game-header"><div class="game-title">{title}</div></div>
  <div class="progress-bar"><div class="progress-fill" id="progressBar" style="width:0%"></div></div>
  <div id="fbContainer">{cards_html}</div>
  <div class="card result-card" id="fbResultCard" style="display:none">
    <div class="score-circle" id="scoreCircle"><span id="fbScoreNum">0</span><div class="score-label">分</div></div>
    <div class="result-text" id="fbResultText"></div>
    <button class="play-again-btn" onclick="location.reload()">再玩一次</button>
  </div>
</div>
<script>
const FB_DATA = {questions_json};
let curFB = 0;
let fbScore = 0;

function showFB(idx) {{
  document.querySelectorAll('.card').forEach(function(c) {{ if (!c.id.includes('fbResult')) c.style.display='none'; }});
  var el = document.getElementById('fb_'+idx);
  if (el) el.style.display = 'block';
  document.getElementById('progressBar').style.width = (idx / FB_DATA.length * 100) + '%';
}}

function checkFB(idx) {{
  var input = document.getElementById('answer_'+idx);
  var userAns = input.value.trim().replace(/\\s+/g, '');
  var correctAns = String(FB_DATA[idx].answer).trim().replace(/\\s+/g, '');
  var resultEl = document.getElementById('fbResult_'+idx);
  var blankEl = document.getElementById('blank_'+idx);
  resultEl.style.display = 'block';
  if (userAns === correctAns) {{
    resultEl.style.background = '#e8f5e9';
    resultEl.style.color = '#1b5e20';
    resultEl.innerHTML = '<strong>&#10004; 正确！</strong>';
    if (blankEl) blankEl.textContent = FB_DATA[idx].answer;
    fbScore++;
    input.disabled = true;
    document.getElementById('fbNext_'+idx).style.display = 'block';
  }} else {{
    resultEl.style.background = '#fce4ec';
    resultEl.style.color = '#b71c1c';
    resultEl.innerHTML = '<strong>&#10006; 不对，再想想！提示：' + FB_DATA[idx].clue + '</strong>';
    input.value = '';
    input.focus();
  }}
}}

function nextFB(idx) {{
  curFB = idx + 1;
  if (curFB >= FB_DATA.length) {{
    showFBResult();
  }} else {{
    showFB(curFB);
  }}
}}

function showFBResult() {{
  document.querySelectorAll('.card').forEach(function(c) {{ c.style.display='none'; }});
  document.getElementById('fbResultCard').style.display = 'block';
  document.getElementById('progressBar').style.width = '100%';
  var pct = Math.round(fbScore / FB_DATA.length * 100);
  var circle = document.getElementById('scoreCircle');
  circle.className = 'score-circle ' + (pct >= 80 ? 'score-full' : pct >= 50 ? 'score-mid' : 'score-low');
  document.getElementById('fbScoreNum').textContent = fbScore;
  var msgs = ['继续加油！', '还不错！', '太棒了！'];
  var msgIdx = pct >= 80 ? 2 : pct >= 50 ? 1 : 0;
  document.getElementById('fbResultText').textContent = `你答对了 ${{fbScore}} / ${{FB_DATA.length}} 题，${{msgs[msgIdx]}}`;
}}
showFB(0);
</script>
</body>
</html>"""
    return SHARED_HEADER + body_script


if __name__ == "__main__":
    try:
        run()
    except Exception as e:
        tb = traceback.format_exc()
        print(json.dumps({
            "success": False,
            "html_path": "",
            "error": f"{type(e).__name__}: {e}\n{tb}"
        }))
        sys.exit(1)
