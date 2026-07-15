import type { Metadata, Viewport } from "next";
import { Inter, Noto_Sans_SC, JetBrains_Mono } from "next/font/google";
import "./globals.css";
import { SmoothScroll } from "@/components/providers/SmoothScroll";
import { CursorGlow } from "@/components/ui/CursorGlow";
import { Navbar } from "@/components/layout/Navbar";
import { Footer } from "@/components/layout/Footer";

const inter = Inter({
  subsets: ["latin"],
  variable: "--font-inter",
  display: "swap",
});

const notoSC = Noto_Sans_SC({
  subsets: ["latin"],
  weight: ["100", "300", "400", "500", "700"],
  variable: "--font-noto-sc",
  display: "swap",
});

const jetbrains = JetBrains_Mono({
  subsets: ["latin"],
  weight: ["300", "400", "500"],
  variable: "--font-jetbrains",
  display: "swap",
});

export const metadata: Metadata = {
  title: "Noosphere — Beyond Intelligence · 项目认知基础设施",
  description:
    "百万上下文给了 AI「看」的能力，Noosphere 给它「理解」的速度。认知图谱 × 记忆引擎 × 运维哨兵，重新定义 AI 与代码的关系。",
  keywords: ["Noosphere", "AI", "认知基础设施", "CodeLens", "Nebula", "DevOps"],
};

export const viewport: Viewport = {
  themeColor: "#050505",
  width: "device-width",
  initialScale: 1,
};

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="zh-CN" className="dark">
      <body
        className={`${inter.variable} ${notoSC.variable} ${jetbrains.variable} noise font-sans`}
      >
        <SmoothScroll>
          <CursorGlow />
          <Navbar />
          <main>{children}</main>
          <Footer />
        </SmoothScroll>
      </body>
    </html>
  );
}
