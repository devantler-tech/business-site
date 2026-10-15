import { error } from "@sveltejs/kit";
export const load = ({ params }) => {
  if (params.locale && params.locale !== "da") error(404);
  return { locale: params.locale || "en" };
};
