type Request = {
  method: string;
  path: string;
  query: string;
  headers: Record<string, string>;
  body: string;
};

type Response<Status extends number, Body extends string> = {
  status: Status;
  body: Body;
};

type Text<Status extends number, Body extends string> = Response<Status, Body> & {
  headers: { "content-type": "text/plain; charset=utf-8" };
};

export type Handle<Req extends Request> = Req["path"] extends "/"
  ? Req["method"] extends "GET" | "HEAD"
    ? Text<200, "ok\n">
    : Text<405, "method not allowed\n"> & { headers: { allow: "GET, HEAD" } }
  : Text<404, "not found\n">;
