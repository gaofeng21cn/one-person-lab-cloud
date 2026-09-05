import { useConsoleController } from "./app/use-console-controller.ts";
import type { PublicConsoleRoute } from "./app/console-router.ts";
import { ConsoleShell } from "./layout/ConsoleShell.tsx";
import { AdminPages } from "./pages/AdminPages.tsx";
import { CustomerPages } from "./pages/CustomerPages.tsx";
import { ForbiddenPage, LoginPage, LogoutRecovery, NotFoundPage, PublicHome, SessionRecovery } from "./pages/PublicPages.tsx";

function assertNever(value: never): never {
  throw new Error(`Unhandled Console route: ${JSON.stringify(value)}`);
}

function PublicPage({ controller, route }: { controller: ReturnType<typeof useConsoleController>; route: PublicConsoleRoute }) {
  switch (route.kind) {
    case "public.home":
      return <PublicHome controller={controller} />;
    case "public.login":
      return <LoginPage controller={controller} />;
    case "public.forbidden":
      return <ForbiddenPage controller={controller} />;
    default:
      return assertNever(route);
  }
}

export default function App() {
  const controller = useConsoleController();

  if (controller.authStatus === "logout_pending" || controller.authStatus === "logout_unconfirmed") {
    return <LogoutRecovery controller={controller} />;
  }
  if (!controller.route) return <NotFoundPage controller={controller} />;
  if (controller.route.surface === "public") return <PublicPage controller={controller} route={controller.route} />;
  if (controller.authStatus !== "ready" || !controller.session) return <SessionRecovery controller={controller} />;

  switch (controller.route.surface) {
    case "customer":
      return <ConsoleShell controller={controller}><CustomerPages controller={controller} route={controller.route} /></ConsoleShell>;
    case "admin":
      return <ConsoleShell controller={controller}><AdminPages controller={controller} route={controller.route} /></ConsoleShell>;
    default:
      return assertNever(controller.route);
  }
}
