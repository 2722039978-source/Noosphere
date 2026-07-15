"use client";

import { useEffect, useRef } from "react";
import * as THREE from "three";
import { usePrefersReducedMotion } from "@/hooks/usePrefersReducedMotion";

export type StageVariant = "lattice" | "torus" | "shard";

// 能量色 —— 紫:神经脉冲 / 橙:能量传输 / 绿:系统健康
const PURPLE = new THREE.Color(0xa855f7);
const ORANGE = new THREE.Color(0xffb347);
const GREEN = new THREE.Color(0x2ee6a6);

/** 按产品构建展示几何体 —— 银白结构为主，accent 蓝仅用于节点等品牌记忆点 */
function buildArtifact(variant: StageVariant, accent: THREE.Color): THREE.Group {
  const group = new THREE.Group();

  const metal = new THREE.MeshStandardMaterial({
    color: 0x11151a,
    metalness: 0.95,
    roughness: 0.18,
    emissive: 0x2b3138, // 中性金属微光
    emissiveIntensity: 0.5,
  });
  const wireMat = new THREE.MeshBasicMaterial({
    color: 0xc9ced6, // 银白线框
    wireframe: true,
    transparent: true,
    opacity: 0.3,
  });

  if (variant === "lattice") {
    // CodeLens —— 知识图谱晶格
    group.add(new THREE.Mesh(new THREE.IcosahedronGeometry(1.45, 1), wireMat));
    group.add(new THREE.Mesh(new THREE.IcosahedronGeometry(0.8, 2), metal));
    // 顶点节点 —— 图谱的"实体"：蓝色基底，紫色推理脉冲沿节点传播
    const nodeGeo = new THREE.SphereGeometry(0.045, 12, 12);
    const base = new THREE.IcosahedronGeometry(1.45, 0);
    const pos = base.getAttribute("position");
    const seen = new Set<string>();
    const pulseNodes: THREE.Mesh<THREE.SphereGeometry, THREE.MeshBasicMaterial>[] = [];
    for (let i = 0; i < pos.count; i++) {
      const key = `${pos.getX(i).toFixed(2)},${pos.getY(i).toFixed(2)},${pos.getZ(i).toFixed(2)}`;
      if (seen.has(key)) continue;
      seen.add(key);
      const node = new THREE.Mesh(nodeGeo, new THREE.MeshBasicMaterial({ color: accent }));
      node.position.set(pos.getX(i), pos.getY(i), pos.getZ(i));
      group.add(node);
      pulseNodes.push(node);
    }
    base.dispose();
    group.userData.pulseNodes = pulseNodes;
  } else if (variant === "torus") {
    // Nebula —— 记忆环流（银白光环）
    group.add(new THREE.Mesh(new THREE.TorusKnotGeometry(0.85, 0.24, 240, 32), metal));
    const halo = new THREE.Mesh(
      new THREE.TorusGeometry(1.7, 0.008, 8, 140),
      new THREE.MeshBasicMaterial({ color: 0xd7dbe0, transparent: true, opacity: 0.3 }),
    );
    halo.rotation.x = Math.PI / 2.4;
    group.add(halo);
    // 琥珀橙能量体沿记忆环传递（位置在主循环中推进）
    const memNode = new THREE.Mesh(
      new THREE.SphereGeometry(0.05, 12, 12),
      new THREE.MeshBasicMaterial({ color: ORANGE, transparent: true, opacity: 0.85 }),
    );
    memNode.position.set(1.7, 0, 0);
    halo.add(memNode);
    group.userData.orbitNode = memNode;
  } else {
    // DevOps —— 哨兵棱晶
    const shard = new THREE.Mesh(new THREE.OctahedronGeometry(1.25, 0), metal);
    group.add(shard);
    const shell = new THREE.Mesh(new THREE.OctahedronGeometry(1.45, 0), wireMat);
    group.add(shell);
    // 哨兵核心 —— 品牌蓝 ↔ 健康绿 呼吸（主循环驱动）
    const core = new THREE.Mesh(
      new THREE.SphereGeometry(0.3, 24, 24),
      new THREE.MeshBasicMaterial({ color: accent, transparent: true, opacity: 0.9 }),
    );
    group.add(core);
    group.userData.healthCore = core;
  }

  return group;
}

/**
 * 产品 3D 展示台 —— 支持鼠标拖动（惯性）、滚轮缩放（阻尼）、
 * 环绕动态灯光与周期性光线扫描。
 */
export default function ProductStage({
  variant,
  accent,
}: {
  variant: StageVariant;
  accent: string;
}) {
  const mountRef = useRef<HTMLDivElement>(null);
  const reduced = usePrefersReducedMotion();

  useEffect(() => {
    const mount = mountRef.current;
    if (!mount) return;

    const accentColor = new THREE.Color(accent);

    const scene = new THREE.Scene();
    const camera = new THREE.PerspectiveCamera(
      45,
      mount.clientWidth / mount.clientHeight,
      0.1,
      50,
    );
    camera.position.set(0, 0.2, 5);

    const renderer = new THREE.WebGLRenderer({
      antialias: true,
      alpha: true,
      powerPreference: "high-performance",
    });
    renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
    renderer.setSize(mount.clientWidth, mount.clientHeight);
    mount.appendChild(renderer.domElement);

    // ── 展品 ──
    const artifact = buildArtifact(variant, accentColor);
    scene.add(artifact);

    // 地台圆环 —— 银白，蓝色不再铺陈背景
    const pedestal = new THREE.Mesh(
      new THREE.RingGeometry(1.9, 1.94, 96),
      new THREE.MeshBasicMaterial({
        color: 0xd7dbe0,
        transparent: true,
        opacity: 0.18,
        side: THREE.DoubleSide,
      }),
    );
    pedestal.rotation.x = -Math.PI / 2;
    pedestal.position.y = -1.6;
    scene.add(pedestal);

    // ── 灯光：冷白环绕主灯 + 轮廓灯 + 一缕品牌蓝补光 ──
    scene.add(new THREE.AmbientLight(0x33383f, 1.4));
    const orbitLight = new THREE.PointLight(0xf5f5f7, 80, 30);
    scene.add(orbitLight);
    const rim = new THREE.DirectionalLight(0xf5f5f7, 2.0);
    rim.position.set(-3, 4, -2);
    scene.add(rim);
    const accentFill = new THREE.PointLight(accentColor, 12, 20);
    accentFill.position.set(0, -3, 3);
    scene.add(accentFill);

    // ── 交互：拖拽旋转（带惯性）+ 滚轮缩放（阻尼趋近）──
    let dragging = false;
    let lastX = 0;
    let lastY = 0;
    let velX = 0;
    let velY = 0;
    let idleTime = 0;
    let zoomTarget = 5;

    const onDown = (e: PointerEvent) => {
      dragging = true;
      lastX = e.clientX;
      lastY = e.clientY;
      mount.setPointerCapture(e.pointerId);
    };
    const onMove = (e: PointerEvent) => {
      if (!dragging) return;
      velX = (e.clientX - lastX) * 0.005;
      velY = (e.clientY - lastY) * 0.005;
      lastX = e.clientX;
      lastY = e.clientY;
      idleTime = 0;
    };
    const onUp = (e: PointerEvent) => {
      dragging = false;
      if (mount.hasPointerCapture(e.pointerId)) mount.releasePointerCapture(e.pointerId);
    };
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      zoomTarget = Math.max(3, Math.min(8, zoomTarget + e.deltaY * 0.0035));
    };

    mount.addEventListener("pointerdown", onDown);
    mount.addEventListener("pointermove", onMove);
    mount.addEventListener("pointerup", onUp);
    mount.addEventListener("pointercancel", onUp);
    mount.addEventListener("wheel", onWheel, { passive: false });

    // ── 可见性与尺寸 ──
    let visible = true;
    const io = new IntersectionObserver(([entry]) => (visible = entry.isIntersecting));
    io.observe(mount);
    const ro = new ResizeObserver(() => {
      camera.aspect = mount.clientWidth / mount.clientHeight;
      camera.updateProjectionMatrix();
      renderer.setSize(mount.clientWidth, mount.clientHeight);
    });
    ro.observe(mount);

    // ── 主循环 ──
    const clock = new THREE.Clock();
    let raf = 0;
    const frame = () => {
      raf = requestAnimationFrame(frame);
      if (!visible) return;

      const dt = clock.getDelta();
      const t = clock.getElapsedTime();
      idleTime += dt;

      // 拖拽惯性 + 闲置自转
      artifact.rotation.y += velX;
      artifact.rotation.x += velY;
      artifact.rotation.x = Math.max(-1.1, Math.min(1.1, artifact.rotation.x));
      velX *= 0.94;
      velY *= 0.94;
      if (!dragging && idleTime > 2) {
        artifact.rotation.y += dt * 0.25; // 缓慢展台自转
      }

      // 悬浮呼吸
      artifact.position.y = Math.sin(t * 0.9) * 0.08;

      // ── 能量表现层 ──
      // 紫色推理脉冲沿晶格节点传播（CodeLens）
      const pulseNodes = artifact.userData.pulseNodes as
        | THREE.Mesh<THREE.SphereGeometry, THREE.MeshBasicMaterial>[]
        | undefined;
      if (pulseNodes) {
        for (let i = 0; i < pulseNodes.length; i++) {
          const p = (Math.sin(t * 1.8 - i * 0.95) + 1) / 2;
          pulseNodes[i].material.color.copy(accentColor).lerp(PURPLE, p * 0.7);
        }
      }
      // 琥珀橙能量体沿记忆环传递 + 亮度脉冲（Nebula）
      const orbitNode = artifact.userData.orbitNode as
        | THREE.Mesh<THREE.SphereGeometry, THREE.MeshBasicMaterial>
        | undefined;
      if (orbitNode) {
        const a = t * 0.9;
        orbitNode.position.set(Math.cos(a) * 1.7, Math.sin(a) * 1.7, 0);
        orbitNode.material.opacity = 0.5 + 0.4 * (Math.sin(t * 3.2) + 1) / 2;
      }
      // 哨兵核心蓝 ↔ 健康绿呼吸（DevOps）
      const healthCore = artifact.userData.healthCore as
        | THREE.Mesh<THREE.SphereGeometry, THREE.MeshBasicMaterial>
        | undefined;
      if (healthCore) {
        healthCore.material.color
          .copy(accentColor)
          .lerp(GREEN, ((Math.sin(t * 1.2) + 1) / 2) * 0.6);
      }

      // 环绕光 —— "光线扫描"：主灯绕展品公转，材质高光随之流动
      orbitLight.position.set(Math.cos(t * 0.6) * 4, 2 + Math.sin(t * 0.3), Math.sin(t * 0.6) * 4);

      // 缩放阻尼趋近
      camera.position.z += (zoomTarget - camera.position.z) * 0.08;
      camera.lookAt(0, 0, 0);

      renderer.render(scene, camera);
    };

    if (reduced) {
      renderer.render(scene, camera);
    } else {
      frame();
    }

    return () => {
      cancelAnimationFrame(raf);
      io.disconnect();
      ro.disconnect();
      mount.removeEventListener("pointerdown", onDown);
      mount.removeEventListener("pointermove", onMove);
      mount.removeEventListener("pointerup", onUp);
      mount.removeEventListener("pointercancel", onUp);
      mount.removeEventListener("wheel", onWheel);
      scene.traverse((obj) => {
        if (obj instanceof THREE.Mesh) {
          obj.geometry.dispose();
          const mats = Array.isArray(obj.material) ? obj.material : [obj.material];
          mats.forEach((m) => m.dispose());
        }
      });
      renderer.dispose();
      mount.removeChild(renderer.domElement);
    };
  }, [variant, accent, reduced]);

  return (
    <div
      ref={mountRef}
      data-lenis-prevent
      className="h-full w-full cursor-grab active:cursor-grabbing"
      aria-label="3D 产品展示：拖动旋转，滚轮缩放"
      role="img"
    />
  );
}
