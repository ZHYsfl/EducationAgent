const fs = require("fs");
const path = require("path");
const dir = "tests/internal/asr";
const files = fs.readdirSync(dir).filter((f) => f.endsWith("_test.go"));

const asrImport = `package asr_test

import (
`;

// Per-file transform: read, replace package, add import block merge
for (const f of files) {
  const fp = path.join(dir, f);
  let s = fs.readFileSync(fp, "utf8");
  s = s.replace(/^package asr\n/m, "package asr_test\n");
  // Insert asr import after first import paren - naive: find `import (` and add voiceagent line if missing
  if (!s.includes(`voiceagent/internal/asr"`)) {
    s = s.replace(
      /import \(\n/,
      `import (
\tasrpkg "voiceagent/internal/asr"
`
    );
  }
  // Replace known exported identifiers (longest first)
  const syms = [
    "NewDouBaoASRClient",
    "NewASRClient",
    "RecognizeStream",
    "StartSession",
    "DouBaoASRConfig",
    "ASRResult",
    "ASRClient",
    "neverReturnASRProvider",
    "blockingASRProvider",
  ];
  for (const sym of syms.sort((a, b) => b.length - a.length)) {
    const re = new RegExp("\\b" + sym.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + "\\b", "g");
    s = s.replace(re, "asrpkg." + sym);
  }
  // Endpoint swap
  s = s.replace(
    /old := doubaoASREndpoint\ndoubaoASREndpoint = ([^\n]+)\ndefer func\(\) \{ doubaoASREndpoint = old \}\(\)/g,
    "restore := asrpkg.SetDouBaoASRWebSocketURLForTest($1)\ndefer restore()"
  );
  s = s.replace(/doubaoASREndpoint = wsURL/g, "asrpkg.SetDouBaoASRWebSocketURLForTest(wsURL");
  // Fix manual patterns
  fs.writeFileSync(fp, s);
  console.log("processed", f);
}
