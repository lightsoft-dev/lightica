import { z } from "zod";

// Lightsoft fork (LIGH-282): a workspace's outside collaborators. Mirrors
// server/internal/handler/lightsoft_guest_api.go.

export interface LightsoftGuestProject {
  id: string;
  title: string;
}

export interface LightsoftGuest {
  email: string;
  /** null until someone signs in with the email. */
  user_id: string | null;
  name: string | null;
  /** null until the invitation is accepted (or after removal). */
  member_id: string | null;
  joined_at: string | null;
  /** Projects the guest is a project member of: all they can see. */
  projects: LightsoftGuestProject[];
}

export interface LightsoftGuestList {
  guests: LightsoftGuest[];
}

const GuestSchema = z.object({
  email: z.string(),
  user_id: z.string().nullish().transform((v) => v ?? null),
  name: z.string().nullish().transform((v) => v ?? null),
  member_id: z.string().nullish().transform((v) => v ?? null),
  joined_at: z.string().nullish().transform((v) => v ?? null),
  projects: z
    .array(z.object({ id: z.string(), title: z.string().catch("") }).loose())
    .catch([]),
});

export const LightsoftGuestListSchema: z.ZodType<LightsoftGuestList> = z
  .object({ guests: z.array(GuestSchema.loose()).catch([]) })
  .loose() as unknown as z.ZodType<LightsoftGuestList>;

export const EMPTY_LIGHTSOFT_GUEST_LIST: LightsoftGuestList = { guests: [] };
