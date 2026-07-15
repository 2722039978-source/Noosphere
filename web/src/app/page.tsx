import { Hero } from "@/components/hero/Hero";
import { SystemsSection } from "@/components/sections/SystemsSection";
import { DataVizSection } from "@/components/sections/DataVizSection";
import { ArchitectureSection } from "@/components/sections/ArchitectureSection";
import { ProductsSection } from "@/components/sections/ProductsSection";
import { RoadmapSection } from "@/components/sections/RoadmapSection";

/**
 * Noosphere 官网单页 —— 章节顺序与导航锚点一致：
 * 首页 → 智能系统 → 实时遥测 → 技术架构 → 产品矩阵 → 未来计划
 */
export default function Home() {
  return (
    <>
      <Hero />
      <SystemsSection />
      <DataVizSection />
      <ArchitectureSection />
      <ProductsSection />
      <RoadmapSection />
    </>
  );
}
