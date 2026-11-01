//! Normalized, JSON-serializable portal data. Unknown optional fields are omitted.
use schemars::JsonSchema;
use serde::{Deserialize, Serialize};
macro_rules! model {
    ($name:ident { $($required:ident),* ; $($optional:ident),* }) => {
        #[derive(Clone, Debug, Serialize, Deserialize, JsonSchema, PartialEq, Eq)]
        pub struct $name {
            $(pub $required: String,)*
            $(#[serde(default, skip_serializing_if="Option::is_none")] pub $optional: Option<String>,)*
        }
    }
}
model!(Document { title; id, category, sent_date });
model!(Notice { title; id, published_date });
model!(ReservationSlot { status; area, date, period });
model!(Request { title; id, subject });
model!(Occurrence { title; id });
#[derive(Clone, Debug, Serialize, Deserialize, JsonSchema, PartialEq, Eq)]
pub struct NoticeDetail {
    #[serde(flatten)]
    pub notice: Notice,
    pub body: String,
    pub warning: String,
}
