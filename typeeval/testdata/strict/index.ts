export type Handle<_Req extends { headers: { "x-required": "yes" } }> = {
  status: 200;
  body: "you may pass\n";
};
