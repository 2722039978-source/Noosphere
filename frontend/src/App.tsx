import { AppProviders } from "@/app/providers";
import { AppRoutes } from "@/app/routes";
import { ChatLifecycle } from "@/features/chat/ChatLifecycle";
import { DesktopTitleBar } from '@/components/DesktopTitleBar';

export default function App() {
  return (
    <AppProviders>
      <div className="flex h-dvh flex-col overflow-hidden">
        <DesktopTitleBar />
        <ChatLifecycle />
        <AppRoutes />
      </div>
    </AppProviders>
  );
}
