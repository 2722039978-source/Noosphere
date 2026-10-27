import { lazy, Suspense, type ReactNode } from "react";
import { Navigate, Route, Routes } from "react-router-dom";
import { AppLayout } from "./layout";
import { LoadingState } from "@/components/ui/feedback";
import { useExperienceStore } from '@/stores/experience-store';
import { ArchiveReturn } from './ArchiveReturn';

const ChatPage = lazy(() => import("@/features/chat/ChatPage").then((m) => ({ default: m.ChatPage })));
const HomePage = lazy(() => import("@/features/home/HomePage").then((m) => ({ default: m.HomePage })));
const GatewayPage = lazy(() => import("@/features/gateway/GatewayPage").then((m) => ({ default: m.GatewayPage })));
const NebulaPage = lazy(() => import("@/features/nebula/NebulaPage").then((m) => ({ default: m.NebulaPage })));
const CodeLensPage = lazy(() => import("@/features/codelens/CodeLensPage").then((m) => ({ default: m.CodeLensPage })));
const DevOpsPage = lazy(() => import("@/features/devops/DevOpsPage").then((m) => ({ default: m.DevOpsPage })));
const SettingsPage = lazy(() => import("@/features/settings/SettingsPage").then((m) => ({ default: m.SettingsPage })));
const ModelHubPage = lazy(() => import('@/features/model-lab/ModelHubPage').then(m => ({ default: m.ModelHubPage })));
const ModelLabPage = lazy(() => import('@/features/model-lab/ModelLabPage').then(m => ({ default: m.ModelLabPage })));
const EvaluationReportPage = lazy(() => import('@/features/model-lab/EvaluationReportPage').then(m => ({ default: m.EvaluationReportPage })));

const ReleaseCenterPage = lazy(() => import('@/features/modelops/ReleaseCenterPage').then(m => ({ default: m.ReleaseCenterPage })));
const ReleaseDetailPage = lazy(() => import('@/features/modelops/ReleaseDetailPage').then(m => ({ default: m.ReleaseDetailPage })));

function Screen({ children }: { children: ReactNode }) {
  return (
    <Suspense fallback={<LoadingState className="h-full" label="正在打开工作区…" />}>
      {children}
    </Suspense>
  );
}

export function AppRoutes() {
  const startPage = useExperienceStore((state) => state.startPage);
  return (
    <ArchiveReturn>{location => <Routes location={location}>
      <Route path="/archive" element={<Navigate to="/home" replace />} />
      <Route path="/home" element={<Screen><HomePage /></Screen>} />
      <Route element={<AppLayout />}>
        <Route path="/" element={<Navigate to={startPage === 'chat' ? '/chat' : '/model-hub'} replace />} />
        <Route path="/chat" element={<Screen><ChatPage /></Screen>} />
        <Route path="/gateway" element={<Screen><GatewayPage /></Screen>} />
        <Route path="/nebula" element={<Screen><NebulaPage /></Screen>} />
        <Route path="/codelens" element={<Screen><CodeLensPage /></Screen>} />
        <Route path="/devops" element={<Screen><DevOpsPage /></Screen>} />
        <Route path="/settings" element={<Screen><SettingsPage /></Screen>} />
        <Route path="/model-hub" element={<Screen><ModelHubPage /></Screen>} />
        <Route path="/model-lab" element={<Screen><ModelLabPage /></Screen>} />
        <Route path="/evaluation-reports" element={<Screen><EvaluationReportPage /></Screen>} />
        <Route path="/evaluation-reports/:id" element={<Screen><EvaluationReportPage /></Screen>} />
        <Route path="/release-center" element={<Screen><ReleaseCenterPage /></Screen>} />
        <Route path="/release-center/:id" element={<Screen><ReleaseDetailPage /></Screen>} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>}</ArchiveReturn>
  );
}
