import { Hero } from "@/components/hero/Hero";
import { SystemsSection } from "@/components/sections/SystemsSection";
import { DataVizSection } from "@/components/sections/DataVizSection";
import { ArchitectureSection } from "@/components/sections/ArchitectureSection";
import { ProductsSection } from "@/components/sections/ProductsSection";
import { RoadmapSection } from "@/components/sections/RoadmapSection";
import { DashboardOverview } from "@/components/dashboard/DashboardOverview";

/**
 * Noosphere 平台首页
 *
 * 上半部分：Dashboard（平台状态 + 模型 + Token + 快捷入口）
 * 下半部分：原有 Landing 内容（系统 / 架构 / 产品 / Roadmap）
 */
export default function Home() {
  return (
    <>
      <div id="hero" />
      <Hero />
      <DashboardOverview />
      <SystemsSection />
      <DataVizSection />
      <ArchitectureSection />
      <ProductsSection />
      <RoadmapSection />
    </>
  );
}
