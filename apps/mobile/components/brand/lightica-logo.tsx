/**
 * Lightsoft fork (LIGH-258): the Lightica mark for the sign-in screens.
 * Vector copy of LIGHTICA_MARK_PATH in packages/ui/components/common/lightica-icon.tsx
 * (mobile does not depend on @multica/ui); change the two together.
 *
 * react-native-svg does not resolve CSS `currentColor`, so the color comes from
 * the theme unless the caller passes one.
 */
import Svg, { Path } from "react-native-svg";
import { THEME } from "@/lib/theme";
import { useColorScheme } from "@/lib/use-color-scheme";
import { LIGHTICA_APP_NAME } from "@/lightica";

const LIGHTICA_MARK_PATH =
  "M437.5 96.5 512 152 298.5 298.5ZM586.5 96.5 512 152 725.5 298.5Z" +
  "M927.5 437.5 872 512 725.5 298.5ZM927.5 586.5 872 512 725.5 725.5Z" +
  "M586.5 927.5 512 872 725.5 725.5ZM437.5 927.5 512 872 298.5 725.5Z" +
  "M96.5 586.5 152 512 298.5 725.5ZM96.5 437.5 152 512 298.5 298.5Z";

interface LighticaLogoProps {
  /** Rendered width and height in points. */
  size?: number;
  color?: string;
}

export function LighticaLogo({ size = 48, color }: LighticaLogoProps) {
  const { isDarkColorScheme } = useColorScheme();
  const resolvedColor =
    color ?? (isDarkColorScheme ? THEME.dark.foreground : THEME.light.foreground);

  return (
    <Svg
      width={size}
      height={size}
      viewBox="96 96 832 832"
      accessibilityLabel={LIGHTICA_APP_NAME}
    >
      <Path d={LIGHTICA_MARK_PATH} fill={resolvedColor} />
    </Svg>
  );
}
