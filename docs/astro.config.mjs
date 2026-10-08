import sitemap from "@astrojs/sitemap";
import starlight from "@astrojs/starlight";
import mermaid from "astro-mermaid";
import { defineConfig, envField } from "astro/config";
import starlightBlog from "starlight-blog";
import starlightGithubAlerts from "starlight-github-alerts";
import starlightLinksValidator from "starlight-links-validator";

export default defineConfig({
  site: "https://devantler.tech",
  // Kept so a renamed page does not break links that already exist in the wild
  // (bookmarks, the platform's own docs, search results). A page rename is a URL
  // change; without an entry here the old URL 404s.
  redirects: {
    "/templates/gitops-tenant-template": "/templates/platform-tenant-template",
  },
  // Build-time feature flags (feature-flag-first delivery, monorepo#2059).
  // The site is a pure static build, so flags are baked at build time: flipping
  // one means a rebuild + redeploy, there is no runtime/per-user evaluation. Use
  // `astro:env` with a Zod-validated schema — type-safe over raw import.meta.env.
  // Convention + lifecycle (remove the gate once shipped) live in docs/README.md.
  env: {
    schema: {
      FEATURE_CLIENT_PORTFOLIO: envField.boolean({
        context: "server",
        access: "public",
        default: true,
      }),
      // Default-off preview notice on the rendered English and Danish homepages.
      // Default-off, so production builds omit it; a preview build enables it
      // with `FEATURE_PREVIEW_BANNER=true npm run build`. Server context = the
      // flag is read while the .astro component renders at build time (SSG), so
      // the banner's HTML is simply not emitted when off — no client JS needed.
      FEATURE_PREVIEW_BANNER: envField.boolean({
        context: "server",
        access: "public",
        default: false,
      }),
    },
  },
  integrations: [
    {
      name: 'devantler-business-pages',
      hooks: {
        'astro:config:setup': ({ injectRoute }) => {
          for (const pattern of ['/', '/da/', '/about/', '/da/about/', '/projects/', '/da/projects/', '/projects/active/', '/projects/completed/']) {
            injectRoute({ pattern, entrypoint: './src/components/business/BusinessPage.astro', prerender: true });
          }
        },
      },
    },
    mermaid(),
    starlight({
      title: "Devantler Tech",
      description:
        "Devantler Tech — a one-person software business building websites, small apps and open-source tools.",
      components: {
        Head: './src/components/business/SupportingHead.astro',
        Header: './src/components/business/SupportingHeader.astro',
        Footer: './src/components/business/SupportingFooter.astro',
        ThemeSelect: './src/components/business/SupportingThemeSelect.astro',
      },
      defaultLocale: "en",
      logo: {
        src: "./src/assets/author.png",
        replacesTitle: false,
      },
      favicon: "/favicon.png",
      social: [
        {
          icon: "linkedin",
          label: "LinkedIn",
          href: "https://www.linkedin.com/in/nikolai-emil-damm-14a786150/",
        },
        {
          icon: "github",
          label: "GitHub",
          href: "https://github.com/devantler",
        },
        {
          icon: "rss",
          label: "RSS",
          href: "/blog/rss.xml",
        },
      ],
      editLink: {
        baseUrl:
          "https://github.com/devantler-tech/business-site/edit/main/docs/",
      },
      customCss: ["./src/styles/custom.css", "./src/styles/business-docs.css"],
      plugins: [
        starlightBlog({
          title: "Devantler Tech Journal",
          authors: {
            devantler: {
              name: "Nikolai Emil Damm",
              title: "Founder & developer, Devantler Tech",
              picture: "/author-avatar.png",
              url: "https://github.com/devantler",
            },
          },
        }),
        starlightGithubAlerts(),
        starlightLinksValidator({
          errorOnRelativeLinks: false,
          exclude: ["/blog/**", "/blog/"],
        }),
      ],
      head: [
        {
          tag: "meta",
          attrs: { property: "og:image", content: "https://devantler.tech/author.png" },
        },
        {
          tag: "meta",
          attrs: { property: "og:type", content: "website" },
        },
        {
          tag: "meta",
          attrs: { name: "twitter:card", content: "summary_large_image" },
        },
        {
          tag: "meta",
          attrs: { name: "twitter:image", content: "https://devantler.tech/author.png" },
        },
        {
          tag: "meta",
          attrs: { name: "author", content: "Nikolai Emil Damm" },
        },
        // Umami privacy-first web analytics (self-hosted on the platform). The
        // website-id is fixed and managed declaratively — the matching Umami
        // "website" is provisioned from Git on the platform (no UI click-ops).
        // data-domains restricts the tracker to the trusted host so the public
        // website-id can't be used to send events from a spoofed site.
        {
          tag: "script",
          attrs: {
            src: "https://analytics.platform.devantler.tech/script.js",
            "data-website-id": "2f8d150e-c6f0-4a90-ab77-431c9ef9dc59",
            "data-domains": "devantler.tech",
            defer: true,
          },
        },
        {
          tag: "script",
          content: `document.addEventListener('DOMContentLoaded', () => {
            const cards = 'article.sl-blog-preview';
            const destination = 'a.sl-blog-preview-link';
            const interactive = 'a, button, input, select, textarea, summary, [role="button"], [role="link"], [contenteditable]';
            function navigate(card) {
              const link = card.querySelector(destination);
              if (link) window.location.href = link.href;
            }
            document.addEventListener('click', (event) => {
              const card = event.target.closest(cards);
              if (!card) return;
              const control = event.target.closest(interactive);
              if (control && control !== card) return;
              navigate(card);
            });
          });`,
        },
      ],
      sidebar: [
        {
          label: "About Devantler Tech",
          link: "/about/",
        },
        {
          label: "Agentic Engineering",
          link: "/agentic-engineering/",
        },
        {
          label: "Projects",
          link: "/projects/",
        },
        {
          label: "Templates",
          items: [{ autogenerate: { directory: "templates" } }],
        },
      ],
      lastUpdated: true,
      pagination: true,
      tableOfContents: { minHeadingLevel: 2, maxHeadingLevel: 3 },
    }),
    sitemap(),
  ],
});
