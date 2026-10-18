import assert from "node:assert/strict";
import { columnFiles, fileLocation } from "../src/data.ts";
import {
  fileAtCell,
  selectionCell,
  visibleCell,
  poolCell,
  cellKey,
  LOOP_COLUMNS,
  LOOP_ROWS,
  wrap,
} from "../src/archive-loop.ts";

// The same eight files and five column categories recur on both sides of zero.
for (let lane = -23; lane <= 23; lane++) {
  const files = columnFiles(wrap(lane, 5));
  for (let row = -35; row <= 35; row++) {
    assert.equal(fileAtCell({ lane, row }), files[wrap(row - 12, 8)]);
  }
}
let checks = 0;
for (const direction of [-1, 1]) {
  let cell = { lane: 2, row: 12 };
  let index = fileAtCell(cell);
  const memory = Array.from({ length: 5 }, (_, lane) => columnFiles(lane)[0]);
  for (let step = 0; step < 10000; step++) {
    const axis = step % 17 < 10 ? "row" : "lane";
    const lane = fileLocation(index).lane;
    if (axis === "row") {
      const files = columnFiles(lane);
      index = files[wrap(files.indexOf(index) + direction, files.length)];
    } else index = memory[wrap(lane + direction, 5)];
    const next = selectionCell(index, cell, { axis, direction });
    assert.equal(
      next[axis] - cell[axis],
      direction,
      "Crossing a seam must move exactly one cell in the requested direction",
    );
    assert.equal(
      fileAtCell(next),
      index,
      "Selected physical cell must contain the requested document",
    );
    memory[fileLocation(index).lane] = index;
    cell = next;
    checks++;
  }
}
for (const center of [
  { lane: 2, row: 12 },
  { lane: -8.3, row: -19.2 },
  { lane: 10002.49, row: -32001.49 },
]) {
  const cells = Array.from({ length: LOOP_COLUMNS * LOOP_ROWS }, (_, i) =>
    visibleCell(i, center),
  );
  assert.equal(new Set(cells.map(cellKey)).size, LOOP_COLUMNS * LOOP_ROWS);
  const lanes = [...new Set(cells.map((c) => c.lane))].sort((a, b) => a - b);
  const rows = [...new Set(cells.map((c) => c.row))].sort((a, b) => a - b);
  assert.equal(lanes.length, LOOP_COLUMNS);
  assert.equal(rows.length, LOOP_ROWS);
  assert.equal(lanes.at(-1) - lanes[0], LOOP_COLUMNS - 1);
  assert.equal(rows.at(-1) - rows[0], LOOP_ROWS - 1);
  assert.ok(lanes[0] < center.lane - 3 && lanes.at(-1) > center.lane + 3);
  assert.ok(rows[0] < center.row - 14 && rows.at(-1) > center.row + 14);
}
for (let i = 0; i < 160; i++) {
  assert.deepEqual(
    poolCell(i),
    { lane: Math.floor(i / 32), row: i % 32 },
    "Reference-animation instance order is preserved",
  );
}
console.log(
  JSON.stringify(
    {
      directionalMoves: checks,
      poolSize: LOOP_COLUMNS * LOOP_ROWS,
      referenceInstances: 160,
      checks: "passed",
    },
    null,
    2,
  ),
);
