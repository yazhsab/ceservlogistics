import { z } from "zod";

const emptyToUndefined = (value: unknown) =>
  value === "" || value === null || value === undefined ? undefined : value;

export const optionalPositiveInteger = z.preprocess(
  emptyToUndefined,
  z.coerce.number().int().positive().optional(),
);

export const optionalNonnegativeInteger = z.preprocess(
  emptyToUndefined,
  z.coerce.number().int().min(0).optional(),
);
