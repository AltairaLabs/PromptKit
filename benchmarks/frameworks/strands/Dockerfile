FROM python:3.12-slim
WORKDIR /app
COPY benchmarks/frameworks/strands/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY benchmarks/frameworks/strands/app.py .
ENV PORT=8093
EXPOSE 8093
CMD ["python", "app.py"]
