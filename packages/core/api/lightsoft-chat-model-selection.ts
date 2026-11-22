import { z } from "zod";

// Lightsoft fork (LIGH-104): a chat session's model / reasoning effort.
// Mirrors server/internal/service/lightsoft_chat_model.go.

/** An empty field means "use the agent's saved value". */
export interface ChatModelSelection {
  model: string;
  thinking_level: string;
}

export interface ChatSessionModelSelectionResponse {
  /** null until a send in the session records a non-default selection. */
  model_selection: ChatModelSelection | null;
}

const ChatModelSelectionSchema = z.object({
  model: z.string().catch(""),
  thinking_level: z.string().catch(""),
});

/** A malformed payload reads as "no selection": the session runs on the
 *  agent's defaults, which is also what an upstream backend does. */
export const ChatSessionModelSelectionResponseSchema: z.ZodType<ChatSessionModelSelectionResponse> =
  z
    .object({
      model_selection: ChatModelSelectionSchema.nullish()
        .catch(null)
        .transform((s) => s ?? null),
    })
    .loose();

export const EMPTY_CHAT_SESSION_MODEL_SELECTION: ChatSessionModelSelectionResponse = {
  model_selection: null,
};

/** Fork-only features the connected backend serves. An upstream backend
 *  404s the route, which reads as all-false. */
export interface LightsoftCapabilities {
  chat_model_selection: boolean;
  /** The caller is an outside collaborator of this workspace (LIGH-282). */
  guest: boolean;
  /** The caller may manage the workspace's outside collaborators. */
  guest_management: boolean;
}

export const LightsoftCapabilitiesSchema: z.ZodType<LightsoftCapabilities> = z
  .object({
    chat_model_selection: z.boolean().catch(false),
    guest: z.boolean().catch(false),
    guest_management: z.boolean().catch(false),
  })
  .loose();

export const NO_LIGHTSOFT_CAPABILITIES: LightsoftCapabilities = {
  chat_model_selection: false,
  guest: false,
  guest_management: false,
};
