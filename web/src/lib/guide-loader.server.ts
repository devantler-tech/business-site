import { error } from "@sveltejs/kit";
import guides from "virtual:technical-guides";

export function loadGuide(path:string) {
  const reading = guides?.find(guide => guide.path === path);
  if (!reading) error(404);
  return {reading,guides:(guides ?? []).map(({path,title}) => ({path,title}))};
}
