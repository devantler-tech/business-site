import PDFDocument from "pdfkit";
import sharp from "sharp";
import interRegular from "@expo-google-fonts/inter/400Regular/Inter_400Regular.ttf?inline";
import interMedium from "@expo-google-fonts/inter/500Medium/Inter_500Medium.ttf?inline";
import interSemibold from "@expo-google-fonts/inter/600SemiBold/Inter_600SemiBold.ttf?inline";
import interBold from "@expo-google-fonts/inter/700Bold/Inter_700Bold.ttf?inline";
import monoRegular from "@expo-google-fonts/roboto-mono/400Regular/RobotoMono_400Regular.ttf?inline";
import monoMedium from "@expo-google-fonts/roboto-mono/500Medium/RobotoMono_500Medium.ttf?inline";
import type { Cv, Entry, Role, SkillGroup } from "../data/cv";

// A4 in PostScript points.
const PAGE = { width: 595.28, height: 841.89 };

// Two-column layout: a dark sidebar in the site's dark theme, a white main column.
const SIDEBAR = { width: 178, pad: 20 };
const SIDEBAR_X = SIDEBAR.pad;
const SIDEBAR_WIDTH = SIDEBAR.width - SIDEBAR.pad * 2;
const FOOTER = { top: PAGE.height - 60 };
const MAIN = { x: SIDEBAR.width + 28, right: 30, top: 40, bottom: 38 };
const MAIN_WIDTH = PAGE.width - MAIN.x - MAIN.right;
const TIMELINE_INDENT = 16;
// The portrait fills a small circle, so it is downscaled to this many pixels square before embedding.
const PORTRAIT = { radius: 46, pixels: 320 };

// Palette mirrors src/styles/custom.css (--sl-color-accent in both themes, the header's
// --sl-color-bg-raw fallback) and Starlight's default gray scale.
const COLOR = {
  sidebarBg: "#111219",
  sidebarText: "#c1c3c8",
  sidebarMuted: "#888c96",
  sidebarRule: "#2a2c36",
  accent: "#39ff14",
  accentOnWhite: "#15803d",
  accentRail: "#cfe9d6",
  text: "#17181c",
  muted: "#555962",
  rule: "#e4e7ee",
  white: "#ffffff",
};

// Fonts and the portrait are imported as data URIs so the renderer has no filesystem layout to depend on.
const dataUriToBuffer = (uri: string) => Buffer.from(uri.slice(uri.indexOf(",") + 1), "base64");

const FONTS = {
  sans: interRegular,
  sansMedium: interMedium,
  sansSemibold: interSemibold,
  sansBold: interBold,
  mono: monoRegular,
  monoMedium: monoMedium,
};
type FontName = keyof typeof FONTS;

interface TextStyle {
  font: FontName;
  size: number;
  color: string;
  lineGap?: number;
  characterSpacing?: number;
}

const STYLE = {
  sectionTitle: { font: "sansBold", size: 9.5, color: COLOR.accentOnWhite, characterSpacing: 1.6 },
  entryTitle: { font: "sansSemibold", size: 10.2, color: COLOR.text },
  entryMeta: { font: "sansMedium", size: 8.3, color: COLOR.muted },
  period: { font: "monoMedium", size: 7.3, color: COLOR.accentOnWhite },
  body: { font: "sans", size: 8.8, color: COLOR.text, lineGap: 1.8 },
  link: { font: "mono", size: 7.3, color: COLOR.accentOnWhite },
  sideLabel: { font: "sansBold", size: 6.6, color: COLOR.accent, characterSpacing: 1.3 },
  sideValue: { font: "sans", size: 7.8, color: COLOR.sidebarText, lineGap: 1.6 },
  sideMuted: { font: "sans", size: 7.2, color: COLOR.sidebarMuted, lineGap: 1.4 },
} satisfies Record<string, TextStyle>;

const displayUrl = (url: string) => url.replace(/^https?:\/\//, "").replace(/\/$/, "");

interface EntryLayout {
  title: string;
  period?: string;
  meta?: string;
  summary?: string;
  bullets?: string[];
  url?: string;
}

/** One line group of an entry: a gap above it, then wrapped text (optionally marked with a bullet). */
interface Block {
  gap: number;
  text: string;
  x: number;
  width: number;
  style: TextStyle;
  link?: string;
  bullet?: boolean;
}

class CvRenderer {
  private readonly doc: PDFKit.PDFDocument;
  private readonly updatedAt: Date;
  private y = MAIN.top;
  private pageIndex = 0;
  private timeline: number[] = [];
  /** Measuring pass: layout runs but nothing is drawn and no page is added. */
  private measuring = false;

  // Sidebar sections flow across pages in this order, each drawn on the first page with room for it.
  private readonly sidebarQueue: Array<[title: string, body: (y: number) => number]>;

  constructor(
    private readonly cv: Cv,
    private readonly photo: Buffer,
  ) {
    this.updatedAt = new Date(cv.updated);
    this.sidebarQueue = [
      ["Contact", (y) => this.sideContact(y)],
      ["Technical Skills", (y) => this.sideSkills(cv.technicalSkills, y)],
      ["Languages", (y) => this.sideValue(cv.languages, y)],
      ["Personal Skills", (y) => this.sideSkills(cv.personalSkills, y)],
      ["Certifications & Badges", (y) => this.sideList(cv.certifications, y)],
      ["Courses", (y) => this.sideList(cv.courses, y)],
      ["Community", (y) => this.sideList(cv.community, y)],
      ["Interests", (y) => this.sideValue(cv.interests, y)],
    ];
    this.doc = new PDFDocument({
      size: [PAGE.width, PAGE.height],
      margin: 0,
      bufferPages: true,
      pdfVersion: "1.7",
      lang: "en",
      displayTitle: true,
      info: {
        Title: `${cv.name} — CV`,
        Author: cv.name,
        Subject: `${cv.title} — curriculum vitae`,
        Keywords: "CV, resume, developer experience, platform engineering, Kubernetes, GitOps",
        Creator: cv.website.label,
        CreationDate: this.updatedAt,
        ModDate: this.updatedAt,
      },
    });
    for (const [name, data] of Object.entries(FONTS)) this.doc.registerFont(name, dataUriToBuffer(data));
  }

  render(): Promise<Buffer> {
    const chunks: Buffer[] = [];
    const done = new Promise<Buffer>((resolve, reject) => {
      this.doc.on("data", (chunk: Buffer) => chunks.push(chunk));
      this.doc.on("end", () => resolve(Buffer.concat(chunks)));
      this.doc.on("error", reject);
    });

    this.drawSidebar();
    this.drawMain();
    if (this.sidebarQueue.length > 0) {
      const left = this.sidebarQueue.map(([title]) => title).join(", ");
      throw new Error(
        `CV sidebar sections did not fit on the ${this.pageIndex + 1} page(s) the main column needs: ${left}. Shorten sidebar content in src/data/cv.ts or reorder the sidebar queue in src/lib/cv-pdf.ts.`,
      );
    }
    this.drawPageNumbers();
    this.doc.end();
    return done;
  }

  // ─── Main column ───

  private drawMain() {
    const { cv } = this;
    this.place(() => {
      this.y += this.text(cv.profile, MAIN.x, this.y, MAIN_WIDTH, STYLE.body);
    });
    this.y += 8;

    this.roles("Experience", cv.experience);
    this.roles("Earlier Experience", cv.earlierExperience);
    this.entries("Education", cv.education);
    this.entries("Talks", cv.talks);
    this.entries("Open Source", cv.openSource);
  }

  private drawSectionHeader(title: string) {
    this.y += 2;
    this.text(title.toUpperCase(), MAIN.x, this.y, MAIN_WIDTH, STYLE.sectionTitle);
    this.y += 13;
    this.line(MAIN.x, this.y, MAIN.x + MAIN_WIDTH, this.y, 0.8, COLOR.rule);
    this.line(MAIN.x, this.y, MAIN.x + 34, this.y, 2, COLOR.accentOnWhite);
    this.y += 12;
  }

  private roles(heading: string, roles: Role[]) {
    roles.forEach((role, i) => {
      const meta = `${role.organisation}, ${role.location}` + (role.engagement ? ` · ${role.engagement}` : "");
      this.entry(
        { title: role.title, meta, period: role.period, summary: role.summary, bullets: role.highlights },
        i === 0 ? heading : undefined,
      );
    });
    this.flushTimeline();
  }

  private entries(heading: string, items: Entry[]) {
    items.forEach((item, i) => {
      this.entry(
        { title: item.title, period: item.period, meta: item.subtitle, summary: item.summary, url: item.url },
        i === 0 ? heading : undefined,
      );
    });
    this.flushTimeline();
  }

  /** Draws one entry, preceded by its section heading when it opens a section, keeping both on one page. */
  private entry(item: EntryLayout, heading?: string) {
    const x = MAIN.x + TIMELINE_INDENT;
    const width = MAIN_WIDTH - TIMELINE_INDENT;
    const periodWidth = item.period ? this.width(item.period, STYLE.period) + 14 : 0;
    // A link without a subtitle takes the subtitle's place, so a project reads as "name → where to find it".
    const linkAsMeta = !item.meta && item.url;
    const link = item.url ? { text: displayUrl(item.url), link: item.url } : undefined;

    const blocks: Block[] = [
      { gap: 0, text: item.title, x, width: width - periodWidth, style: STYLE.entryTitle },
      ...(linkAsMeta && link ? [{ gap: 2, ...link, x, width, style: STYLE.link }] : []),
      ...(item.meta ? [{ gap: 2, text: item.meta, x, width, style: STYLE.entryMeta }] : []),
      ...(item.summary ? [{ gap: 5, text: item.summary, x, width, style: STYLE.body }] : []),
      ...(item.bullets ?? []).map((text) => ({ gap: 3, text, x: x + 11, width: width - 11, style: STYLE.body, bullet: true })),
      ...(!linkAsMeta && link ? [{ gap: 4, ...link, x, width, style: STYLE.link }] : []),
    ];

    this.place(() => {
      if (heading) this.drawSectionHeader(heading);
      if (!this.measuring) this.timeline.push(this.y + STYLE.entryTitle.size * 0.55);
      if (item.period) {
        this.text(item.period, x + width - periodWidth + 14, this.y + 2, periodWidth - 14, STYLE.period, {
          align: "right",
          lineBreak: false,
        });
      }
      for (const block of blocks) {
        this.y += block.gap;
        if (block.bullet) this.circle(x + 3, this.y + STYLE.body.size * 0.55, 1.4, COLOR.accentOnWhite);
        this.y += this.text(block.text, block.x, this.y, block.width, block.style, block.link ? { link: block.link } : {});
      }
      this.y += 8;
    });
  }

  /** Draws the vertical rail through the entry markers collected since the last flush. */
  private flushTimeline() {
    if (this.timeline.length === 0) return;
    const x = MAIN.x + 4;
    const first = this.timeline[0]!;
    const last = this.timeline[this.timeline.length - 1]!;
    if (this.timeline.length > 1) this.line(x, first, x, last, 1.2, COLOR.accentRail);
    for (const y of this.timeline) this.circle(x, y, 2.6, COLOR.accentOnWhite, COLOR.white);
    this.timeline = [];
  }

  /** Measures a main-column block by dry-running it, starts a new page if it would not fit, then draws it. */
  private place(draw: () => void) {
    const start = this.y;
    this.measure(draw);
    const height = this.y - start;
    this.y = start;
    this.ensure(height);
    draw();
  }

  private ensure(height: number) {
    if (this.measuring || this.y + height <= PAGE.height - MAIN.bottom) return;
    this.flushTimeline();
    this.doc.addPage();
    this.pageIndex += 1;
    this.y = MAIN.top;
    this.drawSidebar();
  }

  // ─── Sidebar ───

  private drawSidebar() {
    const { doc } = this;
    doc.rect(0, 0, SIDEBAR.width, PAGE.height).fill(COLOR.sidebarBg);
    doc.rect(0, 0, 3, PAGE.height).fill(COLOR.accent);

    let y = this.pageIndex === 0 ? this.sidebarHero() : this.sidebarCompactHero();
    while (this.sidebarQueue.length > 0) {
      const [title, body] = this.sidebarQueue[0]!;
      const height = this.measure(() => this.sideSection(title, 0, body));
      if (y + height > FOOTER.top) break;
      y = this.sideSection(title, y, body);
      this.sidebarQueue.shift();
    }

    this.sidebarFooter();
  }

  private sidebarHero(): number {
    const { doc, cv } = this;
    const { radius } = PORTRAIT;
    const cx = SIDEBAR.width / 2;
    const cy = MAIN.top + radius;
    doc.save();
    doc.circle(cx, cy, radius).clip();
    doc.image(this.photo, cx - radius, cy - radius, { width: radius * 2, height: radius * 2 });
    doc.restore();
    doc.circle(cx, cy, radius + 1).lineWidth(2).stroke(COLOR.accent);

    let y = cy + radius + 18;
    y += this.text(cv.name, SIDEBAR_X, y, SIDEBAR_WIDTH, { font: "sansBold", size: 14.5, color: COLOR.white, lineGap: 1 }, { align: "center" });
    y += 6;
    y += this.text(cv.title.toUpperCase(), SIDEBAR_X, y, SIDEBAR_WIDTH, { ...STYLE.sideLabel, size: 7 }, { align: "center" });
    return this.sideDivider(y + 14);
  }

  private sidebarCompactHero(): number {
    let y = MAIN.top;
    y += this.text(this.cv.name, SIDEBAR_X, y, SIDEBAR_WIDTH, { font: "sansBold", size: 12.5, color: COLOR.white });
    y += 4;
    y += this.text(this.cv.title.toUpperCase(), SIDEBAR_X, y, SIDEBAR_WIDTH, { ...STYLE.sideLabel, size: 6.4 });
    return this.sideDivider(y + 12);
  }

  private sideDivider(y: number): number {
    this.line(SIDEBAR_X, y, SIDEBAR_X + SIDEBAR_WIDTH, y, 0.6, COLOR.sidebarRule);
    return y + 16;
  }

  private sideSection(title: string, y: number, body: (y: number) => number): number {
    this.text(title.toUpperCase(), SIDEBAR_X, y, SIDEBAR_WIDTH, STYLE.sideLabel);
    this.line(SIDEBAR_X, y + 12, SIDEBAR_X + 18, y + 12, 1.2, COLOR.accent);
    return body(y + 20) + 16;
  }

  private sideContact(y: number): number {
    const { cv } = this;
    const rows: Array<{ label: string; value: string; url?: string }> = [
      { label: "Website", value: cv.website.label, url: cv.website.url },
      { label: "GitHub", value: cv.github.label, url: cv.github.url },
      { label: "LinkedIn", value: cv.linkedin.label, url: cv.linkedin.url },
      { label: "Location", value: cv.location },
    ];
    for (const row of rows) {
      y += this.text(row.label, SIDEBAR_X, y, SIDEBAR_WIDTH, STYLE.sideMuted);
      y += 1 + this.text(row.value, SIDEBAR_X, y + 1, SIDEBAR_WIDTH, STYLE.sideValue, row.url ? { link: row.url } : {});
      y += 6;
    }
    return y - 6;
  }

  private sideSkills(groups: SkillGroup[], y: number): number {
    for (const group of groups) {
      y += this.text(group.label, SIDEBAR_X, y, SIDEBAR_WIDTH, { ...STYLE.sideMuted, font: "sansSemibold", color: COLOR.white });
      y += 1 + this.text(group.value, SIDEBAR_X, y + 1, SIDEBAR_WIDTH, STYLE.sideValue);
      y += 7;
    }
    return y - 7;
  }

  private sideList(items: Entry[], y: number): number {
    for (const item of items) {
      y += this.text(item.title, SIDEBAR_X, y, SIDEBAR_WIDTH, { ...STYLE.sideValue, font: "sansSemibold", color: COLOR.white });
      if (item.subtitle) y += 1 + this.text(item.subtitle, SIDEBAR_X, y + 1, SIDEBAR_WIDTH, STYLE.sideMuted);
      if (item.summary) y += 2 + this.text(item.summary, SIDEBAR_X, y + 2, SIDEBAR_WIDTH, STYLE.sideValue);
      y += 6;
    }
    return y - 6;
  }

  private sideValue(value: string, y: number): number {
    return y + this.text(value, SIDEBAR_X, y, SIDEBAR_WIDTH, STYLE.sideValue);
  }

  private sidebarFooter() {
    const updated = this.updatedAt.toLocaleDateString("en-GB", { month: "long", year: "numeric" });
    this.line(SIDEBAR_X, FOOTER.top + 8, SIDEBAR_X + SIDEBAR_WIDTH, FOOTER.top + 8, 0.6, COLOR.sidebarRule);
    this.text(this.cv.website.label, SIDEBAR_X, FOOTER.top + 20, SIDEBAR_WIDTH, { ...STYLE.link, color: COLOR.accent }, { link: this.cv.website.url });
    this.text(`Updated ${updated}`, SIDEBAR_X, FOOTER.top + 32, SIDEBAR_WIDTH, STYLE.sideMuted);
  }

  private drawPageNumbers() {
    const { doc } = this;
    const range = doc.bufferedPageRange();
    for (let i = 0; i < range.count; i++) {
      doc.switchToPage(range.start + i);
      this.text(`${i + 1} / ${range.count}`, MAIN.x, FOOTER.top + 30, MAIN_WIDTH, { ...STYLE.link, color: COLOR.muted }, { align: "right" });
    }
  }

  // ─── Drawing primitives (all no-ops while measuring) ───

  /** Runs a layout closure without drawing and returns its result. */
  private measure<T>(layout: () => T): T {
    this.measuring = true;
    try {
      return layout();
    } finally {
      this.measuring = false;
    }
  }

  private line(x1: number, y1: number, x2: number, y2: number, lineWidth: number, color: string) {
    if (this.measuring) return;
    this.doc.moveTo(x1, y1).lineTo(x2, y2).lineWidth(lineWidth).stroke(color);
  }

  private circle(x: number, y: number, radius: number, fill: string, stroke?: string) {
    if (this.measuring) return;
    const shape = this.doc.circle(x, y, radius);
    if (stroke) shape.fillAndStroke(fill, stroke);
    else shape.fill(fill);
  }

  private apply(style: TextStyle) {
    this.doc.font(style.font).fontSize(style.size);
  }

  private textOptions(style: TextStyle, width: number, extra: PDFKit.Mixins.TextOptions = {}): PDFKit.Mixins.TextOptions {
    return { width, lineGap: style.lineGap, characterSpacing: style.characterSpacing, ...extra };
  }

  /** Lays out wrapped text at an absolute position, draws it unless measuring, and returns its height. */
  private text(content: string, x: number, y: number, width: number, style: TextStyle, extra: PDFKit.Mixins.TextOptions = {}): number {
    this.apply(style);
    const options = this.textOptions(style, width, extra);
    if (this.measuring) return this.doc.heightOfString(content, options);
    this.doc.fillColor(style.color).text(content, x, y, options);
    return this.doc.y - y;
  }

  private width(content: string, style: TextStyle): number {
    this.apply(style);
    return this.doc.widthOfString(content, { characterSpacing: style.characterSpacing });
  }
}

/** Renders the CV as an A4 PDF; `portraitDataUri` is the photo as a data URI (JPEG or PNG). */
export async function renderCvPdf(cv: Cv, portraitDataUri: string): Promise<Buffer> {
  const photo = await sharp(dataUriToBuffer(portraitDataUri))
    .rotate()
    .resize(PORTRAIT.pixels, PORTRAIT.pixels, { fit: "cover" })
    .jpeg({ quality: 85 })
    .toBuffer();
  return new CvRenderer(cv, photo).render();
}
