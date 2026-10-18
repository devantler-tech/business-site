import type { businessFrame } from "./business.server";

export type BusinessFrame = ReturnType<typeof businessFrame>;
export const businessFrameContext = Symbol("business-frame");
