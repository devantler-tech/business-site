import { prepareReadingReference, captureReadingReference } from "../src/data/journal-reading.mjs";
import { captureJournalFeedReference } from "../src/data/journal-feed-reference.mjs";
import { capturePublicDiscoveryReference } from "../src/data/public-discovery-reference.mjs";

if (process.argv[2] === "prepare") prepareReadingReference();
else if (process.argv[2] === "capture") { await captureReadingReference(); captureJournalFeedReference(); capturePublicDiscoveryReference(); }
else throw new Error("Usage: journal-reading-reference.mjs prepare|capture");
