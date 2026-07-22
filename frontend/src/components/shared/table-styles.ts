const tableHeadClass = "sticky top-0 z-10 bg-background text-xs text-muted-foreground";

const tableClasses = {
  head: "px-4 py-3 font-medium",
  headStart: "py-3 pr-4 font-medium",
  headEnd: "py-3 pl-4 text-right font-medium",
  cell: "px-4 py-3",
  cellStart: "py-3 pr-4",
  cellEnd: "py-3 pl-4 text-right",
} as const;

export { tableClasses, tableHeadClass };
