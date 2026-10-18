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
    <ArchiveReturn startPage={startPage}>{location => <Routes location={location}>
      <Route path="/" element={startPage === 'chat' ? <Navigate to="/chat" replace /> : <Screen><HomePage /></Screen>} />
      <Route path="/home" element={<Screen><HomePage /></Screen>} />
      <Route element={<AppLayout />}>
        <Route path="/chat" element={<Screen><ChatPage /></Screen>} />
        <Route path="/gateway" element={<Screen><GatewayPage /></Screen>} />
        <Route path="/nebula" element={<Screen><NebulaPage /></Screen>} />
        <Route path="/codelens" element={<Screen><CodeLensPage /></Screen>} />
        <Route path="/devops" element={<Screen><DevOpsPage /></Screen>} />
        <Route path="/settings" element={<Screen><SettingsPage /></Screen>} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>}</ArchiveReturn>
  );
}
