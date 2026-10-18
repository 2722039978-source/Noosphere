import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import * as THREE from "three";
import { GLTFLoader } from "three/addons/loaders/GLTFLoader.js";
import { damp } from "../src/motion.ts";
async function load(name) {
  const b = await readFile(
    new URL("../public/assets/" + name, import.meta.url),
  );
  return (
    await new GLTFLoader().parseAsync(
      b.buffer.slice(b.byteOffset, b.byteOffset + b.byteLength),
      "",
    )
  ).scene;
}
const [original, assembly] = await Promise.all([
  load("archive-cassette.glb"),
  load("archive-assembly.glb"),
]);
const parts = new Map();
const vertices = (scene, collect = false) => {
  const points = new Map();
  scene.updateMatrixWorld(true);
  scene.traverse((mesh) => {
    if (!mesh.isMesh) return;
    const surface = mesh.material.name.replace(/\.\d+$/, "");
    if (surface === "Carbon_Ink") return;
    if (collect) {
      assert.ok(
        mesh.userData.assemblyPart,
        "Every mesh belongs to a physical assembly",
      );
      parts.set(
        mesh.userData.assemblyPart,
        (parts.get(mesh.userData.assemblyPart) || 0) + 1,
      );
    }
    const position = mesh.geometry.attributes.position;
    for (let i = 0; i < position.count; i++) {
      const point = new THREE.Vector3()
        .fromBufferAttribute(position, i)
        .applyMatrix4(mesh.matrixWorld);
      points.set(surface + ":" + point.toArray().join(","), { surface, point });
    }
  });
  return points;
};
const a = vertices(original),
  b = vertices(assembly, true);
assert.deepEqual([...parts.keys()].sort(), [
  "carrier",
  "cover",
  "fasteners",
  "optical-core",
  "optical-lenses",
  "substrate",
]);
// Compare actual distances: rounding to a fixed grid can split equivalent
// float32 coordinates on either side of a rounding boundary after Blender joins.
function maxVertexError(from, to) {
  const surfaces = new Map();
  for (const { surface, point } of to.values()) {
    if (!surfaces.has(surface)) surfaces.set(surface, []);
    surfaces.get(surface).push(point);
  }
  let max = 0;
  for (const { surface, point } of from.values()) {
    const candidates = surfaces.get(surface) || [];
    let nearest = Infinity;
    for (const candidate of candidates)
      nearest = Math.min(nearest, point.distanceToSquared(candidate));
    max = Math.max(max, Math.sqrt(nearest));
  }
  return max;
}
const vertexError = Math.max(maxVertexError(a, b), maxVertexError(b, a));
assert.ok(
  vertexError < 1e-5,
  `Regrouping must retain assembled geometry within float32 tolerance: ${vertexError}`,
);
const height = new THREE.Box3()
  .setFromObject(assembly)
  .getSize(new THREE.Vector3()).y;
assert.ok(Math.abs(height - 3.7) < 1e-5);
// Interrupted motion retains position and velocity, then converges exactly enough
// for the viewer to snap to the original assembly pose.
const spread = { value: 0, velocity: 0 };
for (let i = 0; i < 25; i++) damp(spread, 1, 5.5, 1 / 60);
const interrupted = spread.value;
damp(spread, 0, 5.5, 1 / 60);
assert.ok(
  Math.abs(spread.value - interrupted) < 0.05,
  "Reassembly must not jump on reversal",
);
for (let i = 0; i < 180; i++) damp(spread, 0, 5.5, 1 / 60);
assert.ok(Math.abs(spread.value) < 0.0001 && Math.abs(spread.velocity) < 0.001);
console.log(
  JSON.stringify(
    {
      parts: Object.fromEntries(parts),
      uniqueSurfaceVertices: a.size,
      vertexError,
      modelHeight: height,
      reassembly: spread.value,
      checks: "passed",
    },
    null,
    2,
  ),
);
