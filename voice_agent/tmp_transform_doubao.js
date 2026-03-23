const fs = require("fs");
const path = "tests/internal/doubao/doubao_proto_test.go";
let s = fs.readFileSync(path, "utf8");
s = s.replace("package doubao\n", "package doubao_test\n");
s = s.replace(
  'import (\n\t"encoding/binary"\n\t"testing"\n)',
  'import (\n\t"encoding/binary"\n\t"testing"\n\n\tdb "voiceagent/internal/doubao"\n)'
);
const syms = [
  "MsgTypeFullClientReq",
  "MsgTypeAudioOnlyReq",
  "MsgTypeFullServerResp",
  "MsgTypeAudioOnlyResp",
  "MsgTypeError",
  "FlagNegSeq",
  "FlagPosSeq",
  "FlagLastData",
  "FlagNoSeq",
  "ParseServerResponse",
  "ParseAudioResponse",
  "ParseErrorResponse",
  "ParseHeader",
  "BuildHeader",
  "BuildFrame",
  "SerJSON",
  "SerNone",
  "CompGzip",
  "CompNone",
];
syms.sort((a, b) => b.length - a.length);
for (const sym of syms) {
  const re = new RegExp("\\b" + sym.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + "\\b", "g");
  s = s.replace(re, "db." + sym);
}
s = s.replace(/gzipCompress/g, "db.GzipCompress");
fs.writeFileSync(path, s);
console.log("ok");
