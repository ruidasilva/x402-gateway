FROM node:20-alpine

WORKDIR /app

COPY package.json package-lock.json* ./
RUN npm install --production

COPY examples/ ./examples/
COPY frontend-playground/ ./frontend-playground/

ENV PORT=3000
EXPOSE 3000

CMD ["node", "examples/server.js"]
