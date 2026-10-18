import * as THREE from "three";

type Surface = THREE.MeshPhysicalMaterial;
type Palette = { high: Surface; low?: Surface };

// The array and selected file share geometry. Morph their surface properties
// on one mesh so transparent shells never overlap during a quality change.
export class CardAppearance {
  private palettes = new Map<string, Palette>();

  register(name: string, high: Surface, low?: Surface) {
    this.palettes.set(name, { high, low });
  }

  prepare(group: THREE.Group) {
    for (const child of group.children) {
      const mesh = child as THREE.Mesh;
      const name = mesh.userData.surface as string;
      const palette = this.palettes.get(name);
      if (!palette) continue;
      const mat = palette.high.clone();
      const amount = { value: 0 };
      mesh.material = mat;
      mesh.userData.appearance = amount;
      mat.onBeforeCompile = (shader) => {
        shader.uniforms.archiveQuality = amount;
        shader.fragmentShader =
          "uniform float archiveQuality;\n" + shader.fragmentShader;
        if (name === "Frosted_Polymer") {
          shader.vertexShader =
            "varying float vArchiveHeight;\n" + shader.vertexShader;
          shader.vertexShader = shader.vertexShader.replace(
            "#include <begin_vertex>",
            "#include <begin_vertex>\nvArchiveHeight = position.y / 3.7;",
          );
          shader.fragmentShader =
            "varying float vArchiveHeight;\n" + shader.fragmentShader;
          shader.fragmentShader = shader.fragmentShader.replace(
            "#include <color_fragment>",
            "#include <color_fragment>\ndiffuseColor.rgb *= mix(mix(vec3(0.40, 0.30, 0.20), vec3(1.0, 0.98, 0.94), smoothstep(0.1, 1.0, vArchiveHeight)), vec3(1.0), archiveQuality);",
          );
          shader.fragmentShader = shader.fragmentShader.replace(
            "#include <roughnessmap_fragment>",
            "#include <roughnessmap_fragment>\nroughnessFactor = mix(0.28, mix(0.48, 0.035, smoothstep(0.36, 0.68, vArchiveHeight)), archiveQuality);",
          );
        } else if (!palette.low) {
          // Stable screen-space coverage adds internal geometry without an
          // abrupt visibility toggle or a second transparent body.
          shader.fragmentShader = shader.fragmentShader.replace(
            "#include <color_fragment>",
            "#include <color_fragment>\nfloat coverage = fract(52.9829189 * fract(dot(gl_FragCoord.xy, vec2(0.06711056, 0.00583715))));\nif (archiveQuality <= coverage) discard;",
          );
        }
      };
      mat.customProgramCacheKey = () =>
        `archive-surface-${name}-${Boolean(palette.low)}`;
    }
  }

  apply(group: THREE.Group, value: number) {
    for (const child of group.children) {
      const mesh = child as THREE.Mesh;
      const palette = this.palettes.get(mesh.userData.surface);
      if (!palette) {
        // The printed canvas belongs to this file, including returning copies.
        (mesh.material as THREE.MeshBasicMaterial).opacity = value;
        continue;
      }
      mesh.userData.appearance.value = value;
      const { high, low } = palette;
      if (!low) continue;
      const mat = mesh.material as Surface;
      mat.color.copy(low.color).lerp(high.color, value);
      if (
        mat.attenuationColor &&
        low.attenuationColor &&
        high.attenuationColor
      ) {
        mat.attenuationColor
          .copy(low.attenuationColor)
          .lerp(high.attenuationColor, value);
        mat.attenuationDistance =
          Number.isFinite(low.attenuationDistance) &&
          Number.isFinite(high.attenuationDistance)
            ? THREE.MathUtils.lerp(
                low.attenuationDistance,
                high.attenuationDistance,
                value,
              )
            : high.attenuationDistance;
      }
      for (const key of [
        "roughness",
        "metalness",
        "transmission",
        "thickness",
        "clearcoat",
        "clearcoatRoughness",
      ] as const) {
        mat[key] = THREE.MathUtils.lerp(low[key] ?? 0, high[key] ?? 0, value);
      }
      // Keep the same transmission shader/pass throughout the transition.
      if (high.transmission > 0)
        mat.transmission = Math.max(0.000001, mat.transmission);
    }
  }

  dispose(group: THREE.Group) {
    for (const child of group.children) {
      const mesh = child as THREE.Mesh;
      const mat = mesh.material as THREE.MeshBasicMaterial;
      if (!mesh.userData.surface) mat.map?.dispose();
      mat.dispose();
    }
  }
}
