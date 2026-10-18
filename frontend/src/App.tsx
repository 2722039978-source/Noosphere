import { AppProviders } from "@/app/providers";
import { AppRoutes } from "@/app/routes";
import { ChatLifecycle } from "@/features/chat/ChatLifecycle";

export default function App() {
  return (
    <AppProviders>
      <div className="flex h-dvh flex-col overflow-hidden">
        <ChatLifecycle />
        <AppRoutes />
      </div>
    </AppProviders>
  );
}
