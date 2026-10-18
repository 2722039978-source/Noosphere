import { ArchiveScene } from "/src/scene.ts";
import { selectionWave as baselineWave } from "./baseline-motion.ts";
import { selectionWave as currentWave } from "/src/motion.ts";
import { columnFiles } from "/src/data.ts";
import { wrap } from "/src/archive-loop.ts";

const $ = (s) => document.querySelector(s),
  duration = 192,
  dt = 1 / 60,
  startTime = 8;
const scenes = [
  new ArchiveScene($("#before .scene"), currentWave, false),
  new ArchiveScene($("#after .scene"), currentWave, true),
];
const draws = scenes.map((s) => s.composer.render.bind(s.composer));
// Seek by simulating at 60 Hz; only the requested frame is drawn.
scenes.forEach((s) => {
  s.composer.render = () => {};
});
const fields = [
  "lift",
  "rail",
  "shoulder",
  "laneFocus",
  "columnCamera",
  "selectedCell",
  "coordinateOrigin",
  "pulses",
  "pendingPulse",
  "rotation",
  "targetRotation",
  "returnY",
  "detail",
  "targetDetail",
  "reveal",
  "targetReveal",
  "last",
  "clock",
  "idleGain",
  "pulseGain",
  "lastInteraction",
  "scanTime",
  "scanBlend",
  "selectedSlot",
  "looping",
  "canInspect",
  "clearance",
];
let saved = [],
  frame = 0,
  index = 0,
  events = [],
  samples = [],
  maxFrame = 0,
  playing = false,
  ready = false,
  lastWall = 0,
  accumulator = 0;
const files = columnFiles(2);
const schedules = {
  next: [[1, 1]],
  previous: [[1, -1]],
  repeat: [
    [1, 1],
    [19, 1],
    [37, 1],
    [55, 1],
  ],
  reverse: [
    [1, 1],
    [25, -1],
  ],
};

function snapshot(s) {
  return {
    state: Object.fromEntries(fields.map((k) => [k, structuredClone(s[k])])),
    camera: s.camera.position.clone(),
    aim: s.cameraAim.clone(),
    fov: s.camera.fov,
  };
}
function reset() {
  scenes.forEach((s, i) => {
    s.setMode("hidden");
    for (const key of fields) s[key] = structuredClone(saved[i].state[key]);
    s.camera.position.copy(saved[i].camera);
    s.cameraAim.copy(saved[i].aim);
    s.camera.fov = saved[i].fov;
    s.camera.lookAt(s.cameraAim);
    s.camera.updateProjectionMatrix();
    s.camera.updateMatrixWorld();
    s.drawLabel(0);
    s.pointer.set(0, 0);
  });
  frame = 0;
  index = 0;
}
function advance() {
  frame++;
  const action = events.find(([f]) => f === frame);
  if (action) {
    const direction = action[1];
    index = files[wrap(files.indexOf(index) + direction, files.length)];
    scenes.forEach((s) => s.select(index, { axis: "row", direction }));
  }
  scenes.forEach((s) => s.update(startTime + frame * dt));
}
function setPlaying(value) {
  playing = value;
  accumulator = 0;
  lastWall = 0;
  $("#play").textContent = value ? "暂停" : "同步播放";
}
function seek(target) {
  target = Math.max(0, Math.min(duration, target));
  if (target <= frame) reset();
  while (frame < target) advance();
  // The first frame is the checkpoint, prior to the first input.
  if (target === 0) {
    scenes.forEach((s) => s.update(startTime));
    reset();
  }
  render();
}
function render() {
  scenes.forEach((s, i) => {
    draws[i]();
    const [x, y] = s.projectCard(-2.5, 3.7),
      view = $(["#before", "#after"][i]);
    const zoom = $("#zoom").checked ? 1.7 : 1;
    const px = (x / 1920 - 0.34) * zoom + 0.34,
      py = (y / 1080 - 0.3) * zoom + 0.3;
    const marker = view.querySelector(".marker");
    marker.style.left = `${px * 100}%`;
    marker.style.top = `${py * 100}%`;
    $(["#before-phase", "#after-phase"][i]).textContent = s.pendingPulse
      ? "档案抬起中"
      : s.pulses.length
        ? "波浪传播中"
        : "就位";
  });
  $("#timeline").value = String(frame);
  $("#stamp").textContent =
    `${(frame * dt).toFixed(2)} / ${(duration * dt).toFixed(2)} 秒`;
  const delta = scenes[1].model.position.y - scenes[0].model.position.y;
  const replayError = Math.max(
    Math.abs(scenes[0].model.position.y - samples[frame].before),
    Math.abs(scenes[1].model.position.y - samples[frame].after),
  );
  $("#check").textContent =
    replayError < 1e-7 ? "同步回放校验通过" : "回放校验异常";
  $("#check").dataset.replayError = String(replayError);
  $("#difference").textContent =
    `${delta >= 0 ? "+" : ""}${delta.toFixed(3)} · ${((Math.abs(delta) / 3.7) * 100).toFixed(1)}%`;
  const max = samples[maxFrame].after - samples[maxFrame].before;
  $("#explanation").textContent =
    Math.abs(delta) < 0.001
      ? `此刻两版几乎重合。最大高度差出现在 ${(maxFrame * dt).toFixed(2)} 秒，为卡片高度的 ${((Math.abs(max) / 3.7) * 100).toFixed(1)}%。`
      : `右侧选中档案比左侧${delta >= 0 ? "高" : "低"} ${Math.abs(delta).toFixed(3)} 个场景单位，占卡片高度的 ${((Math.abs(delta) / 3.7) * 100).toFixed(1)}%。观察右侧先抬起档案，再带动周围起伏的顺序。`;
  const x = 56 + (frame / duration) * 1044;
  $("#cursor").setAttribute("x1", x);
  $("#cursor").setAttribute("x2", x);
}
function chart() {
  // The zero checkpoint still owns the old file. Plot from the first input so
  // changing ownership is not misrepresented as a downward motion of one file.
  const origin = samples[1].before,
    values = samples
      .slice(1)
      .flatMap((p) => [p.before - origin, p.after - origin]);
  const min = Math.floor((Math.min(...values) - 0.03) * 10) / 10,
    max = Math.ceil((Math.max(...values) + 0.03) * 10) / 10;
  const x = (f) => 56 + (f / duration) * 1044,
    y = (v) => 154 - ((v - origin - min) / (max - min)) * 130;
  const path = (key) =>
    samples
      .slice(1)
      .map(
        (p, f) =>
          `${f ? "L" : "M"}${x(f + 1).toFixed(2)},${y(p[key]).toFixed(2)}`,
      )
      .join(" ");
  let markup = "";
  for (let i = 0; i <= 4; i++) {
    const v = min + ((max - min) * i) / 4,
      py = y(v + origin);
    markup += `<path d="M56 ${py}H1100" stroke="#cfcabe" stroke-width=".6"/><text x="44" y="${py + 4}" text-anchor="end" fill="#77796b" font-size="11">${v.toFixed(2)}</text>`;
  }
  for (let i = 0; i <= duration; i += 30)
    markup += `<text x="${x(i)}" y="180" text-anchor="middle" fill="#77796b" font-size="11">${(i * dt).toFixed(1)}s</text>`;
  for (const [f] of events)
    markup += `<path d="M${x(f)} 18V154" stroke="#9d9c8e" stroke-dasharray="3 5"/>`;
  markup += `<path d="${path("before")}" fill="none" stroke="#a66044" stroke-width="2.3"/><path d="${path("after")}" fill="none" stroke="#577356" stroke-width="2.3"/><line id="cursor" x1="56" x2="56" y1="16" y2="155" stroke="#353b30"/>`;
  $("#chart").innerHTML = markup;
}
function prepare() {
  setPlaying(false);
  events = schedules[$("#scenario").value];
  scenes[0].selectionPulse =
    $("#baseline").value === "original" ? baselineWave : currentWave;
  reset();
  // The checkpoint has already been rendered into the selected model.
  scenes.forEach((s) => s.update(startTime));
  reset();
  samples = [
    { before: scenes[0].model.position.y, after: scenes[1].model.position.y },
  ];
  while (frame < duration) {
    advance();
    samples.push({
      before: scenes[0].model.position.y,
      after: scenes[1].model.position.y,
    });
  }
  maxFrame = samples.reduce(
    (best, p, f) =>
      Math.abs(p.after - p.before) >
      Math.abs(samples[best].after - samples[best].before)
        ? f
        : best,
    0,
  );
  chart();
  seek(0);
}
$("#play").onclick = () => {
  if (frame === duration) seek(0);
  setPlaying(!playing);
};
$("#restart").onclick = () => {
  seek(0);
  setPlaying(true);
};
$("#prev").onclick = () => {
  setPlaying(false);
  seek(frame - 1);
};
$("#next").onclick = () => {
  setPlaying(false);
  seek(frame + 1);
};
$("#peak").onclick = () => {
  setPlaying(false);
  seek(maxFrame);
};
$("#timeline").oninput = (e) => {
  setPlaying(false);
  seek(Number(e.target.value));
};
$("#scenario").onchange = () => {
  if (ready) {
    prepare();
    setPlaying(true);
  }
};
$("#baseline").onchange = () => {
  if (ready) {
    prepare();
    setPlaying(true);
  }
};
$("#zoom").onchange = () => {
  document
    .querySelectorAll(".view")
    .forEach((v) => v.classList.toggle("zoomed", $("#zoom").checked));
  if (ready) render();
};
$("#chart").onclick = (e) => {
  if (!ready) return;
  const rect = $("#chart").getBoundingClientRect();
  setPlaying(false);
  seek(
    Math.round(
      ((((e.clientX - rect.left) / rect.width) * 1120 - 56) / 1044) * duration,
    ),
  );
};
function resize() {
  scenes.forEach((s) => {
    s.resize();
    // These are real-size comparison panels, not the scaled 1920px app stage.
    const ratio = Math.min(devicePixelRatio, 1.5);
    s.renderer.setPixelRatio(ratio);
    s.composer.setPixelRatio(ratio);
  });
  if (ready) render();
}
addEventListener("resize", resize);
document.addEventListener("keydown", (e) => {
  if (!ready || ["INPUT", "SELECT", "BUTTON", "A"].includes(e.target.tagName))
    return;
  if (e.code === "Space") {
    e.preventDefault();
    $("#play").click();
  }
  if (e.code === "ArrowLeft" || e.code === "ArrowRight") {
    e.preventDefault();
    setPlaying(false);
    seek(frame + (e.code === "ArrowLeft" ? -1 : 1));
  }
});
function tick(wall) {
  if (playing && ready && !document.hidden) {
    if (lastWall)
      accumulator +=
        Math.min((wall - lastWall) / 1000, 0.1) * Number($("#speed").value);
    const count = Math.floor(accumulator / dt);
    if (count) {
      accumulator -= count * dt;
      let target = frame + count;
      if (target > duration) {
        if ($("#loop").checked) {
          reset();
          target %= duration;
        } else {
          target = duration;
          setPlaying(false);
        }
      }
      seek(target);
    }
  }
  lastWall = wall;
  requestAnimationFrame(tick);
}
try {
  await document.fonts.ready;
  await Promise.all(scenes.map((s) => s.load()));
  scenes.forEach((s) => {
    s.setMode("archive");
    s.select(0);
    for (let f = 1; f <= 480; f++) s.update(f * dt);
  });
  // Start both runs with the exact same resting state and no residual pulses.
  scenes.forEach((s) => {
    s.pulses = [];
    s.idleGain = 0;
    s.lastInteraction = startTime;
    s.update(startTime);
  });
  saved = scenes.map(snapshot);
  resize();
  ready = true;
  document
    .querySelectorAll(".transport button,#timeline")
    .forEach((el) => (el.disabled = false));
  prepare();
  setPlaying(true);
  requestAnimationFrame(tick);
} catch (error) {
  $("#explanation").textContent = `场景加载失败：${error.message}`;
  $("#play").textContent = "加载失败";
  console.error(error);
}
