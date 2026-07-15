"use client";

import { useEffect, useRef } from "react";
import * as THREE from "three";
import { usePrefersReducedMotion } from "@/hooks/usePrefersReducedMotion";

/**
 * Hero 三维场景 —— "认知核心"装置
 *
 * 构成：
 *  - 2600 粒子星云（加性混合，冷蓝双色渐变）
 *  - 中央二十面体线框核心 + 金属内核（开场自下方缓慢升起）
 *  - 两道倾斜轨道环（反向旋转）
 *  - 鼠标视差相机
 *
 * 性能策略：
 *  - DPR 钳制 ≤ 2；离屏 / 切后台自动暂停 rAF
 *  - 卸载时完整 dispose；prefers-reduced-motion 时只渲染静帧
 */
export default function ParticleField() {
  const mountRef = useRef<HTMLDivElement>(null);
  const reduced = usePrefersReducedMotion();

  useEffect(() => {
    const mount = mountRef.current;
    if (!mount) return;

    // ── 基础三件套 ──
    const scene = new THREE.Scene();
    scene.fog = new THREE.FogExp2(0x050505, 0.075);

    const camera = new THREE.PerspectiveCamera(
      55,
      mount.clientWidth / mount.clientHeight,
      0.1,
      100,
    );
    camera.position.set(0, 0.4, 7);

    const renderer = new THREE.WebGLRenderer({
      antialias: true,
      alpha: true,
      powerPreference: "high-performance",
    });
    renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
    renderer.setSize(mount.clientWidth, mount.clientHeight);
    mount.appendChild(renderer.domElement);

    // ── 粒子星云 —— 冷白为主体，蓝/紫/绿/橙作为稀疏"能量粒子"点缀 ──
    const COUNT = 2600;
    const positions = new Float32Array(COUNT * 3);
    const colors = new Float32Array(COUNT * 3);
    const cBlue = new THREE.Color(0x00a8ff);
    const cWhite = new THREE.Color(0xf5f5f7);
    const cSilver = new THREE.Color(0x9ba1a6);
    const cPurple = new THREE.Color(0xa855f7);
    const cGreen = new THREE.Color(0x2ee6a6);
    const cOrange = new THREE.Color(0xffb347);
    for (let i = 0; i < COUNT; i++) {
      const r = 3.5 + Math.random() * 7;
      const theta = Math.random() * Math.PI * 2;
      const phi = Math.acos(2 * Math.random() - 1);
      positions[i * 3] = r * Math.sin(phi) * Math.cos(theta);
      positions[i * 3 + 1] = r * Math.sin(phi) * Math.sin(theta) * 0.45; // 压扁成星盘
      positions[i * 3 + 2] = r * Math.cos(phi);
      const roll = Math.random();
      let c: THREE.Color;
      if (roll < 0.08) c = cBlue.clone().lerp(cWhite, Math.random() * 0.35);
      else if (roll < 0.11) c = cPurple.clone().lerp(cWhite, Math.random() * 0.4);
      else if (roll < 0.13) c = cGreen.clone().lerp(cWhite, Math.random() * 0.4);
      else if (roll < 0.14) c = cOrange.clone().lerp(cWhite, Math.random() * 0.4);
      else c = cWhite.clone().lerp(cSilver, Math.random() * 0.7);
      colors[i * 3] = c.r;
      colors[i * 3 + 1] = c.g;
      colors[i * 3 + 2] = c.b;
    }
    const pGeo = new THREE.BufferGeometry();
    pGeo.setAttribute("position", new THREE.BufferAttribute(positions, 3));
    pGeo.setAttribute("color", new THREE.BufferAttribute(colors, 3));
    const pMat = new THREE.PointsMaterial({
      size: 0.028,
      vertexColors: true,
      transparent: true,
      opacity: 0.85,
      blending: THREE.AdditiveBlending,
      depthWrite: false,
      sizeAttenuation: true,
    });
    const particles = new THREE.Points(pGeo, pMat);
    scene.add(particles);

    // ── 认知核心 —— 银白线框结构 + 少量蓝色节点 ──
    const core = new THREE.Group();

    const wire = new THREE.Mesh(
      new THREE.IcosahedronGeometry(1.35, 1),
      new THREE.MeshBasicMaterial({
        color: 0xc9ced6,
        wireframe: true,
        transparent: true,
        opacity: 0.26,
      }),
    );
    core.add(wire);

    // 顶点节点 —— 神经网络：品牌蓝基底，紫色脉冲沿节点相位传播
    const nodeGeo = new THREE.SphereGeometry(0.028, 10, 10);
    const nodeBase = new THREE.IcosahedronGeometry(1.35, 0);
    const nodePos = nodeBase.getAttribute("position");
    const seenNodes = new Set<string>();
    const pulseNodes: THREE.Mesh<THREE.SphereGeometry, THREE.MeshBasicMaterial>[] = [];
    let nodeIdx = 0;
    for (let i = 0; i < nodePos.count; i++) {
      const key = `${nodePos.getX(i).toFixed(2)},${nodePos.getY(i).toFixed(2)},${nodePos.getZ(i).toFixed(2)}`;
      if (seenNodes.has(key)) continue;
      seenNodes.add(key);
      nodeIdx++;
      if (nodeIdx % 2 !== 0) continue; // 只保留一半顶点 —— "少量"节点
      const node = new THREE.Mesh(
        nodeGeo,
        new THREE.MeshBasicMaterial({ color: 0x00a8ff, transparent: true, opacity: 0.9 }),
      );
      node.position.set(nodePos.getX(i), nodePos.getY(i), nodePos.getZ(i));
      wire.add(node);
      pulseNodes.push(node);
    }
    nodeBase.dispose();

    const inner = new THREE.Mesh(
      new THREE.IcosahedronGeometry(0.62, 2),
      new THREE.MeshStandardMaterial({
        color: 0x11151a,
        metalness: 0.92,
        roughness: 0.22,
        emissive: 0x073a5c, // 核心能量源：蓝 ↔ 紫 缓慢流动（见主循环）
        emissiveIntensity: 1.1,
      }),
    );
    core.add(inner);
    const emissiveBlue = new THREE.Color(0x073a5c);
    const emissivePurple = new THREE.Color(0x3d1a66);

    const ringMat1 = new THREE.MeshBasicMaterial({
      color: 0xd7dbe0, // 银白主轨道环
      transparent: true,
      opacity: 0.3,
    });
    const ring1 = new THREE.Mesh(new THREE.TorusGeometry(2.05, 0.012, 8, 160), ringMat1);
    ring1.rotation.x = Math.PI / 2.25;
    core.add(ring1);

    const ringMat2 = new THREE.MeshBasicMaterial({
      color: 0x00a8ff, // 副环保留一缕品牌蓝
      transparent: true,
      opacity: 0.14,
    });
    const ring2 = new THREE.Mesh(new THREE.TorusGeometry(2.65, 0.008, 8, 160), ringMat2);
    ring2.rotation.x = Math.PI / 1.75;
    ring2.rotation.y = Math.PI / 5;
    core.add(ring2);

    // 轨道能量体 —— 琥珀橙脉冲沿主环传递，青绿同步点沿副环巡航
    const comet = new THREE.Mesh(
      new THREE.SphereGeometry(0.032, 10, 10),
      new THREE.MeshBasicMaterial({ color: 0xffb347, transparent: true, opacity: 0.85 }),
    );
    comet.position.set(2.05, 0, 0);
    ring1.add(comet);
    const syncDot = new THREE.Mesh(
      new THREE.SphereGeometry(0.024, 10, 10),
      new THREE.MeshBasicMaterial({ color: 0x2ee6a6, transparent: true, opacity: 0.6 }),
    );
    syncDot.position.set(2.65, 0, 0);
    ring2.add(syncDot);

    core.position.y = reduced ? 0.15 : -3.2; // 开场从画面下方升起
    scene.add(core);

    // ── 灯光 —— 冷白主光源，蓝色仅作轮廓微光 ──
    scene.add(new THREE.AmbientLight(0x2c3036, 1.2));
    const key = new THREE.PointLight(0xf5f5f7, 55, 24);
    key.position.set(3, 3, 4);
    scene.add(key);
    const rim = new THREE.PointLight(0x00a8ff, 14, 20);
    rim.position.set(-4, -2, -3);
    scene.add(rim);

    // ── 鼠标视差 ──
    const mouse = { x: 0, y: 0 };
    const onPointer = (e: PointerEvent) => {
      mouse.x = (e.clientX / window.innerWidth) * 2 - 1;
      mouse.y = (e.clientY / window.innerHeight) * 2 - 1;
    };
    window.addEventListener("pointermove", onPointer, { passive: true });

    // ── 可见性感知：离屏 / 切后台即停帧 ──
    let visible = true;
    let pageVisible = true;
    const io = new IntersectionObserver(([entry]) => {
      visible = entry.isIntersecting;
    });
    io.observe(mount);
    const onVis = () => {
      pageVisible = document.visibilityState === "visible";
    };
    document.addEventListener("visibilitychange", onVis);

    // ── 自适应尺寸 ──
    const ro = new ResizeObserver(() => {
      const w = mount.clientWidth;
      const h = mount.clientHeight;
      camera.aspect = w / h;
      camera.updateProjectionMatrix();
      renderer.setSize(w, h);
    });
    ro.observe(mount);

    // ── 主循环 ──
    const clock = new THREE.Clock();
    const RISE_DURATION = 2.6;
    let raf = 0;

    const frame = () => {
      raf = requestAnimationFrame(frame);
      if (!visible || !pageVisible) return;

      const t = clock.getElapsedTime();

      // 核心升起（easeOutExpo）
      if (!reduced && t < RISE_DURATION + 0.1) {
        const p = Math.min(t / RISE_DURATION, 1);
        core.position.y = -3.2 + (0.15 + 3.2) * (1 - Math.pow(2, -10 * p));
      }

      particles.rotation.y = t * 0.02;
      wire.rotation.y = t * 0.12;
      wire.rotation.x = t * 0.05;
      inner.rotation.y = -t * 0.2;
      ring1.rotation.z = t * 0.18;
      ring2.rotation.z = -t * 0.12;
      core.position.y += Math.sin(t * 0.8) * 0.0009; // 悬浮呼吸

      // 核心能量呼吸 —— 蓝(能量源) ↔ 紫(思考过程) 缓慢流动
      inner.material.emissiveIntensity = 1.1 + Math.sin(t * 1.6) * 0.3;
      inner.material.emissive.lerpColors(
        emissiveBlue,
        emissivePurple,
        (Math.sin(t * 0.5) + 1) / 2,
      );

      // 紫色脉冲沿神经网络节点传播（相位差形成行波）
      for (let i = 0; i < pulseNodes.length; i++) {
        const p = (Math.sin(t * 1.8 - i * 1.05) + 1) / 2;
        pulseNodes[i].material.color.copy(cBlue).lerp(cPurple, p * 0.75);
        pulseNodes[i].material.opacity = 0.55 + p * 0.4;
      }

      // 琥珀橙能量脉冲（随主环公转，亮度呼吸）+ 青绿同步点
      (comet.material as THREE.MeshBasicMaterial).opacity = 0.45 + 0.4 * (Math.sin(t * 3) + 1) / 2;
      (syncDot.material as THREE.MeshBasicMaterial).opacity = 0.3 + 0.35 * (Math.sin(t * 2.2 + 1.5) + 1) / 2;

      // 相机视差（阻尼跟随）
      camera.position.x += (mouse.x * 0.65 - camera.position.x) * 0.03;
      camera.position.y += (0.4 - mouse.y * 0.35 - camera.position.y) * 0.03;
      camera.lookAt(0, 0, 0);

      renderer.render(scene, camera);
    };

    if (reduced) {
      renderer.render(scene, camera); // 静帧
    } else {
      frame();
    }

    // ── 清理 ──
    return () => {
      cancelAnimationFrame(raf);
      window.removeEventListener("pointermove", onPointer);
      document.removeEventListener("visibilitychange", onVis);
      io.disconnect();
      ro.disconnect();
      scene.traverse((obj) => {
        if (obj instanceof THREE.Mesh || obj instanceof THREE.Points) {
          obj.geometry.dispose();
          const mats = Array.isArray(obj.material) ? obj.material : [obj.material];
          mats.forEach((m) => m.dispose());
        }
      });
      renderer.dispose();
      mount.removeChild(renderer.domElement);
    };
  }, [reduced]);

  return <div ref={mountRef} className="absolute inset-0" aria-hidden />;
}
