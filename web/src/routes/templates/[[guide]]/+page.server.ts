import { error } from "@sveltejs/kit";
import { isRetiredGuide } from "../../../retired-guides.ts";

/** Every former template guide forwards to its retired-guide destination; unknown names are 404. */
export const load = ({params}:{params:{guide?:string}}) => {
  const path = `/templates/${params.guide ? `${params.guide}/` : ""}`;
  if (!isRetiredGuide(path)) error(404);
  return {path};
};
