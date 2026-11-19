import { cn } from "../../lib/utils";

// Lightsoft fork (LIGH-253): the Lightica mark, traced from the desktop app
// icon's artwork (lightsoft/desktop/icon-source.png). Eight triangles, four-fold
// symmetric around (512, 512); drawn in currentColor so it follows the theme.
const LIGHTICA_MARK_PATH =
  "M437.5 96.5 512 152 298.5 298.5ZM586.5 96.5 512 152 725.5 298.5Z" +
  "M927.5 437.5 872 512 725.5 298.5ZM927.5 586.5 872 512 725.5 725.5Z" +
  "M586.5 927.5 512 872 725.5 725.5ZM437.5 927.5 512 872 298.5 725.5Z" +
  "M96.5 586.5 152 512 298.5 725.5ZM96.5 437.5 152 512 298.5 298.5Z";

interface LighticaIconProps extends React.ComponentProps<"svg"> {
  /** Rendered size in px. Omit to size with `className` (1em default). */
  size?: number;
  /** Accessible name. Without one the mark is decorative. */
  title?: string;
}

export function LighticaIcon({ size, title, className, ...props }: LighticaIconProps) {
  return (
    <svg
      viewBox="96 96 832 832"
      width={size ?? "1em"}
      height={size ?? "1em"}
      fill="currentColor"
      role={title ? "img" : undefined}
      aria-hidden={title ? undefined : true}
      aria-label={title}
      className={cn("shrink-0 text-foreground", className)}
      {...props}
    >
      {title ? <title>{title}</title> : null}
      <path d={LIGHTICA_MARK_PATH} />
    </svg>
  );
}
