import { error } from "@sveltejs/kit";
export const load = ({ params }: { params: { legacy: string } }) => {
  if ((params.legacy !== "active" && params.legacy !== "completed")) error(404);
  return { fallback: params.legacy === "active" ? "#open-title" : "#research" };
};
