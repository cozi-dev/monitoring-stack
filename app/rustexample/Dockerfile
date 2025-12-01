# Use a rust base image
FROM rust:1.9.1-slim as builder

# Set the working directory inside the container
WORKDIR /app

# Copy the project files into the container
COPY . .

# Install cargo-watch
RUN cargo install cargo-watch

# Start the application with cargo-watch for hot reload
CMD ["cargo", "watch", "-x", "run"]
