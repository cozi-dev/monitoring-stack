use opentelemetry::{
    global,
    propagation::Extractor,
    trace::{Span, Tracer, TracerProvider as _, TraceContextExt},
    KeyValue,
};
use opentelemetry_otlp::WithExportConfig;
use opentelemetry_sdk::{propagation::TraceContextPropagator, trace::BatchSpanProcessor, Resource};
use rocket::request::{self, FromRequest, Request};
use tracing::{info, warn, error, debug, info_span};
use tracing_subscriber::{layer::SubscriberExt, util::SubscriberInitExt};

#[macro_use]
extern crate rocket;

struct RocketHeaderExtractor<'a>(&'a rocket::http::HeaderMap<'a>);

impl<'a> Extractor for RocketHeaderExtractor<'a> {
    fn get(&self, key: &str) -> Option<&str> {
        self.0.get_one(key)
    }

    fn keys(&self) -> Vec<&str> {
        // Return an empty vector to satisfy the trait, as we don't need full key iteration
        vec![]
    }
}

#[rocket::async_trait]
impl<'r> FromRequest<'r> for RocketHeaderExtractor<'r> {
    type Error = ();

    async fn from_request(req: &'r Request<'_>) -> request::Outcome<Self, Self::Error> {
        request::Outcome::Success(RocketHeaderExtractor(req.headers()))
    }
}

#[get("/")]
fn index(extractor: RocketHeaderExtractor<'_>) -> &'static str {
    // Extract parent context from incoming HTTP headers using the global propagator
    let parent_cx = global::get_text_map_propagator(|prop| {
        prop.extract(&extractor)
    });

    let tracer = global::tracer("rustexample");
    let mut otel_span = tracer.start_with_context("Handle index request", &parent_cx);
    otel_span.set_attribute(KeyValue::new("index", "my-value"));
    otel_span.add_event(
        "Main span event".to_string(),
        vec![KeyValue::new("foo", "1")],
    );
    
    // Get trace_id and span_id from the OpenTelemetry span
    let span_context = otel_span.span_context();
    let trace_id = span_context.trace_id().to_string();
    let span_id = span_context.span_id().to_string();
    
    // Create a tracing span with trace_id and span_id fields
    let _tracing_span = info_span!(
        "index_handler",
        trace_id = %trace_id,
        span_id = %span_id,
        otel.name = "index",
        otel.kind = "server"
    ).entered();
    
    // Structured logging with trace context - now logs will include trace_id and span_id
    info!(
        endpoint = "/",
        method = "GET",
        custom_attribute = "my-value",
        "Processing index request"
    );
    
    debug!("Index handler started processing");
    
    // Example of logging with different levels
    warn!(event = "example_warning", "This is a warning log with trace context");
    
    otel_span.end();

    "Hello, world!"
}

#[launch]
#[tokio::main]
async fn rocket() -> _ {
    // Initialize tracing subscriber with OpenTelemetry layer
    init_logging().expect("Failed to initialize logging");
    
    let _tracer = init_tracer().expect("Failed to initialize tracer.");

    info!(
        service = "rustexample",
        port = 8080,
        "Starting Rust example service"
    );

    rocket::build()
        .configure(
            rocket::Config::figment()
                .merge(("port", 8080))
                .merge(("address", "0.0.0.0")),
        )
        .mount("/", routes![index])
}

fn init_logging() -> Result<(), Box<dyn std::error::Error>> {
    // Initialize the tracing subscriber with JSON formatting
    let telemetry_layer = tracing_opentelemetry::layer();
    
    tracing_subscriber::registry()
        .with(
            tracing_subscriber::fmt::layer()
                .json()
                .with_current_span(true)
                .with_span_list(true)
                .with_target(true)
                .with_thread_ids(true)
                .with_thread_names(true)
        )
        .with(tracing_subscriber::EnvFilter::from_default_env()
            .add_directive(tracing::Level::INFO.into()))
        .with(telemetry_layer)
        .init();
    
    Ok(())
}

fn init_tracer() -> Result<opentelemetry_sdk::trace::Tracer, Box<dyn std::error::Error>> {
    let exporter = opentelemetry_otlp::SpanExporter::builder()
        .with_tonic()
        .with_endpoint("http://tempo:55690")
        .build()?;

    let batch = BatchSpanProcessor::builder(exporter).build();

    let resource = Resource::builder().with_service_name("rustexample").build();

    let provider = opentelemetry_sdk::trace::SdkTracerProvider::builder()
        .with_span_processor(batch)
        .with_resource(resource)
        .build();

    let tracer = provider.tracer("rustexample");
    global::set_tracer_provider(provider);
    global::set_text_map_propagator(TraceContextPropagator::new());
    Ok(tracer)
}
