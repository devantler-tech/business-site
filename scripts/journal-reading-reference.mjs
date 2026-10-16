import { prepareReadingReference, captureReadingReference } from "../src/data/journal-reading.mjs";

if (process.argv[2] === "prepare") prepareReadingReference();
else if (process.argv[2] === "capture") captureReadingReference();
else throw new Error("Usage: journal-reading-reference.mjs prepare|capture");
