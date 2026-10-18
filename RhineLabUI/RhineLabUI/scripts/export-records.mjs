import fs from "node:fs/promises";
import ts from "typescript";

const source = await fs.readFile(
  new URL("../src/data.ts", import.meta.url),
  "utf8",
);
const { outputText } = ts.transpileModule(source, {
  compilerOptions: {
    module: ts.ModuleKind.ESNext,
    target: ts.ScriptTarget.ES2022,
  },
});
const { records } = await import(
  `data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`
);
const output = new URL("../public/archives/", import.meta.url);
await fs.mkdir(output, { recursive: true });
for (const r of records) {
  const text = `RHINE LAB · INTERNAL DATABASE\nFILE ${r.id} / ${r.title}\n${r.en}\n\n科室：${r.department}\n编目范围：${r.date}\n相关人物：${r.lead}\n访问范围：${r.clearance}\n\n${r.abstract}\n\n研究记录\n${r.findings.map((f, i) => `${i + 1}. ${f}`).join("\n")}\n\n设定参考：${r.source}\n本文为基于公开设定的档案式改写，非游戏原文。\n`;
  await fs.writeFile(
    new URL(`RHINE-LAB-${r.id}.txt`, output),
    "\uFEFF" + text,
    "utf8",
  );
}
console.log(`Prepared ${records.length} downloadable archive records.`);
