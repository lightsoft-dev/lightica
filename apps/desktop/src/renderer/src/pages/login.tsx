import { LoginPage } from "@multica/views/auth";
import { DragStrip } from "@multica/views/platform";
import { LighticaIcon } from "@multica/ui/components/common/lightica-icon";
import { useT } from "@multica/views/i18n";

export function DesktopLoginPage() {
  const { t } = useT("auth");

  return (
    <div className="flex h-screen flex-col">
      <DragStrip />
      <LoginPage
        // Lightsoft fork (LIGH-253): Lightica signs in under its own name and mark.
        logo={<LighticaIcon size={48} />}
        title={t(($) => $.signin.title).replace("Multica", "Lightica")}
        onSuccess={() => {
          // Auth store update triggers AppContent re-render → shows DesktopShell.
          // Initial workspace navigation happens in routes.tsx via IndexRedirect.
        }}
        // Lightsoft fork (LIGH-257): no Google button. The team server has no
        // Google OAuth, so it only opened the browser login; the email code
        // signs in here directly.
      />
    </div>
  );
}
