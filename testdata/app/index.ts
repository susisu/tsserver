type Text<Status extends number, Body extends string> = {
  status: Status;
  headers: { "content-type": "text/plain; charset=utf-8" };
  body: Body;
};

export type Handle<Req> = Req extends { path: "/" }
  ? Req extends { method: "GET" }
    ? Text<200, "ok\n">
    : Text<405, "method not allowed\n">
  : Text<404, "not found\n">;
